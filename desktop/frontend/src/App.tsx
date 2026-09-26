import { HelpTopics } from "./ContextHelp";
import { OperationAccess } from "./OperationAccess";
import { PrivacyDisclosure, SupportGuidance } from "./PrivacyDisclosure";
import { HubPanel } from "./HubPanel";
import { RunnerPanel } from "./RunnerPanel";
import { RunComparison } from "./RunComparison";
import { Baseline } from "./Baseline";
import { NoteDraft } from "./NoteDraft";
import { Recovery, RetainedDrafts } from "./Recovery";
import { RunPanel } from "./RunPanel";
import { RunExplanation } from "./RunExplanation";
import { ReplayPanel } from "./ReplayPanel";
import { PacketPanel } from "./PacketPanel";
import { PrivacyPanel } from "./PrivacyPanel";
import { ProtectionPanel } from "./ProtectionPanel";
import { SuitePanel, type SuiteRunHandoff } from "./SuitePanel";
import { EnvironmentPanel } from "./EnvironmentPanel";
import { Reduction, type ReductionForm } from "./Reduction";
import { onRetentionResult, savedId } from "./drafting";
import { IndicatorsContext, useLifecycle, useWindowBusy } from "./lifecycle";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactElement } from "react";
import {
  authorTest,
  buildReproducer,
  compareReproducers,
  cancel,
  compare,
  type CompareResult,
  createSampleWorkspace,
  discardEditorDraft,
  editReproducer,
  saveTest,
  suggestExpectations,
  approveExpectations,
  undoReproducer,
  type TestAnswer,
  type TestDraftDocument,
  type TestResult,
  type TestReview,
  type TestSuggestionRequest,
  type ReproducerPlan,
  type ReproducerComparisonResult,
  type ReproducerResult,
  type ReproducerStep,
  filters as readFilters,
  guide as readGuide,
  runPractice,
  type GuideResult,
  type GuideTrialId,
  type PracticeResult,
  editorDrafts as readEditorDrafts,
  saveEditorDraft,
  openCase,
  openGrid,
  openSequence,
  openCorrelationReview,
  decideCorrelation,
  runDiagnosis,
  openDiagnosisReport,
  openDiagnosisGroupsReport,
  groupDiagnoses,
  reviewFindings,
  decideFindings,
  normalizeCompare,
  type DiagnosisRequest,
  type DiagnosisResult,
  type DiagnosisGroupsResult,
  type FindingReviewRequest,
  type FindingReviewResult,
  type FindingStatus,
  type GroupDiagnosesRequest,
  type NormalizeResult,
  type CorrelationReviewResult,
  type SequenceResult,
  openReview,
  previewTransformation,
  saveTransformPlan,
  openTransformPlan,
  previewReduction,
  startReduction,
  type ReviewResult,
  type TransformResult,
  type TransformPlanResult,
  type TransformStep,
  type ReductionResult,
  inspectOccurrence,
  type InspectionResult,
  openProjectOverview,
  createProject,
  updateProjectSettings,
  registerCase,
  registerRevision,
  updateRegisteredCase,
  openWorkspace,
  buildIndex,
  describeIndex,
  type BuildIndexRequest,
  type IndexResult,
  recentWorkspaces,
  forgetWorkspace,
  openRevisions,
  captureSample,
  recordView,
  recoverSession,
  type EditorDraft,
  type RecoveryResult,
  saveFilter,
  search,
  selectFilter,
  selectWorkspace,
  shell,
  type CaseResult,
  type CommandId,
  type FiltersResult,
  type GridResult,
  type Indicator,
  type CaseChange,
  type CaseRegistration,
  type Match,
  type ProjectOverviewResult,
  type SettingsChange,
  type RecentResult,
  type RevisionsResult,
  type RegionId,
  type SearchResult,
  type Shell,
  type State,
  type Theme,
  type WorkspaceResult,
} from "./bindings";
import { Comparison, readsComparison } from "./Comparison";
import { Review } from "./Review";
import { GuidedSample } from "./GuidedSample";
import { Sequence } from "./Sequence";
import { Inspector } from "./Inspector";
import { Reproducer, type ReproducerView } from "./Reproducer";
import { RevisionComparison, type ComparisonSeed } from "./RevisionComparison";
import { AssertionSetAuthoring } from "./AssertionSetAuthoring";
import { CanonicalTestEditor } from "./CanonicalTestEditor";
import { ProfileEditor } from "./ProfileEditor";
import { ScenarioPanel } from "./ScenarioPanel";
import { TestAuthoring, type PromotionProvenance, type TestView } from "./TestAuthoring";
import { Diagnosis } from "./Diagnosis";
import { Badge, MessageGrid, Palette, Report, Separator, Status } from "./shell";
import { VocabularyContext } from "./vocabulary";
import { ProjectPanel } from "./ProjectPanel";
import { ImportPanel } from "./ImportPanel";
import { CapturePanel } from "./CapturePanel";
import { ObservationPanel } from "./ObservationPanel";
import { MaintenancePanel } from "./MaintenancePanel";
import { RawInspection } from "./RawInspection";
import { PerformanceCorpus } from "./PerformanceCorpus";
import { RecentWorkspaces } from "./RecentWorkspaces";
import type { CaptureObservationBinding } from "./bindings";
import { TaskPanel, TaskTabs } from "./TaskTabs";
import { IconButton } from "./IconButton";
import { DemoCallout, NewProjectForm } from "./Home";
import {
  EmptyState,
  GLOBAL_DESTINATIONS,
  Modal,
  MoreMenu,
  NavItem,
  PROJECT_DESTINATIONS,
  Page,
  folderName,
  humanize,
  type Destination,
} from "./layout";

/** The panes never collapse to nothing: either one keeps a usable share of the
 * window, whether it is dragged or moved with a keyboard. */
const MIN_SPLIT = 20;
const MAX_SPLIT = 80;
const SPLIT_STEP = 5;

/** Verifying a case, reading a project and searching all run to completion once
 * they start, so Cancel is offered only while an interruptible operation runs:
 * choosing a folder, or grouping the findings of several cases. */
type Running =
  | "workspace"
  | "case"
  | "project"
  | "search"
  | "grid"
  | "filters"
  | "inspect"
  | "reproducer"
  | "authoring"
  | "comparison"
  | "revisions"
  | "practice"
  | "sequence"
  | "correlation-review"
  | "transformation"
  | "transform-plan"
  | "transform-open"
  | "reduction-preview"
  | "reduction"
  | "review"
  | "diagnosis"
  | "diagnosis-report"
  | "diagnosis-groups"
  | "diagnosis-groups-report"
  | "finding-review"
  | "normalize"
  | "recent"
  | "sample";

/** The views inside a page. Each page keeps every view mounted, so an edit in
 * one survives looking at another. */
type CaseView = "messages" | "timeline" | "findings";
/** What a person does to a case, each on its own page with a way back. */
type CaseFlow = "test" | "replay" | "compare" | "reproduce" | "reduce";
const CASE_FLOW_TITLES: Record<CaseFlow, string> = {
  test: "New test",
  replay: "Replay",
  compare: "Compare",
  reproduce: "Reproducer",
  reduce: "Reduce",
};
type TestsView = "suites" | "tests" | "assertions" | "profiles" | "scenarios" | "baselines";
type RunsView = "run" | "details" | "compare";
type ReportsView = "packets" | "disclosure" | "export" | "notes";
type ToolsView = "inspect" | "benchmarks";
type SettingsView = "general" | "license" | "team" | "runners" | "security" | "storage";

const CASE_VIEWS: { key: CaseView; label: string }[] = [
  { key: "messages", label: "Messages" },
  { key: "timeline", label: "Timeline" },
  { key: "findings", label: "Findings" },
];
const TESTS_VIEWS: { key: TestsView; label: string }[] = [
  { key: "suites", label: "Suites" },
  { key: "tests", label: "Test files" },
  { key: "assertions", label: "Assertion sets" },
  { key: "profiles", label: "Profiles" },
  { key: "scenarios", label: "Scenarios" },
  { key: "baselines", label: "Baselines" },
];
const RUNS_VIEWS: { key: RunsView; label: string }[] = [
  { key: "run", label: "Run a test" },
  { key: "details", label: "Run details" },
  { key: "compare", label: "Compare runs" },
];
const REPORTS_VIEWS: { key: ReportsView; label: string }[] = [
  { key: "packets", label: "Packets" },
  { key: "disclosure", label: "Disclosure review" },
  { key: "export", label: "Transform and export" },
  { key: "notes", label: "Notes" },
];
const TOOLS_VIEWS: { key: ToolsView; label: string }[] = [
  { key: "inspect", label: "Inspect file" },
  { key: "benchmarks", label: "Benchmarks" },
];
const SETTINGS_VIEWS: { key: SettingsView; label: string }[] = [
  { key: "general", label: "General" },
  { key: "license", label: "License" },
  { key: "team", label: "Team" },
  { key: "runners", label: "Runners" },
  { key: "security", label: "Security" },
  { key: "storage", label: "Storage" },
];

/** What the status line says while one of this window's operations runs. */
const PROGRESS: Record<Running, string> = {
  workspace: "Opening the folder…",
  case: "Verifying the case…",
  project: "Reading the project…",
  search: "Searching this project…",
  grid: "Reading messages…",
  filters: "Saving the filter…",
  inspect: "Reading the message…",
  reproducer: "Updating the reproducer…",
  authoring: "Updating the test…",
  comparison: "Comparing…",
  revisions: "Comparing revisions…",
  practice: "Running the demo test…",
  sequence: "Loading the timeline…",
  "correlation-review": "Reviewing correlations…",
  transformation: "Previewing the transformation…",
  "transform-plan": "Saving the transformation plan…",
  "transform-open": "Opening the transformation plan…",
  "reduction-preview": "Previewing the reduction…",
  reduction: "Running the reduction…",
  review: "Reading the export review…",
  diagnosis: "Running the diagnosis…",
  "diagnosis-report": "Opening the report…",
  "diagnosis-groups": "Grouping findings…",
  "diagnosis-groups-report": "Opening the grouping report…",
  "finding-review": "Reviewing findings…",
  normalize: "Comparing under the policy…",
  recent: "Updating recent projects…",
  sample: "Importing the demo fixtures…",
};

export default function App() {
  const [described, setDescribed] = useState<Shell | null>(null);
  // How many rows one window of each paged view asks for: the facade's own
  // bounds, as its description publishes them.
  const bounds = described?.vocabulary.bounds;
  const [windowState, setWindowState] = useState<State>("busy");
  const [windowReason, setWindowReason] = useState<string | undefined>(undefined);

  // One operation runs at a time here as well as in the facade, and it holds
  // the window's one slot: while it runs, the rest of the window is
  // unavailable rather than answered busy. The practice run and a reduction
  // are the operations here a panel's own cancel control stops.
  const { running, run, cancel: cancelRunning } = useLifecycle<Running>({
    window: true,
    names: { practice: "practice", reduction: "reduction" },
  });
  // Each counts the palette's requests to open that screen in the inspector.
  const [rawRequest, setRawRequest] = useState(0);
  const [corpusRequest, setCorpusRequest] = useState(0);
  const [workspace, setWorkspace] = useState<WorkspaceResult | null>(null);
  const [evidence, setEvidence] = useState<CaseResult | null>(null);
  const [investigation, setInvestigation] = useState<ProjectOverviewResult | null>(null);
  const [editable, setEditable] = useState<RevisionsResult | null>(null);
  const [recent, setRecent] = useState<RecentResult | null>(null);
  const [found, setFound] = useState<SearchResult | null>(null);
  const [savedFilters, setSavedFilters] = useState<FiltersResult | null>(null);
  const [inspectionResult, setInspectionResult] = useState<InspectionResult | null>(null);
  const [selectedOccurrence, setSelectedOccurrence] = useState<string | null>(null);
  const [gridResult, setGridResult] = useState<GridResult | null>(null);
  const [indexResult, setIndexResult] = useState<IndexResult | null>(null);
  const [reproducerResult, setReproducerResult] = useState<ReproducerView | null>(null);
  const [testResult, setTestResult] = useState<TestView | null>(null);
  const [runSpecPath, setRunSpecPath] = useState<string | undefined>(undefined);
  const [runEnvironment, setRunEnvironment] = useState<string | undefined>(undefined);
  // The suite handoff the run view was last seeded from, with the folder it
  // names: shown beside the run view so the person sees which prepared
  // result and release pins they came from, and that the run view applies
  // neither.
  const [suiteHandoff, setSuiteHandoff] = useState<{ workspace: string; handoff: SuiteRunHandoff } | null>(null);
  const [comparisonResult, setComparisonResult] = useState<CompareResult | null>(null);
  const [revisionResult, setRevisionResult] = useState<ReproducerComparisonResult | null>(null);
  // The build the reproducer panel last handed to the revision comparison.
  const [comparisonSeed, setComparisonSeed] = useState<ComparisonSeed | null>(null);
  const [guideResult, setGuideResult] = useState<GuideResult | null>(null);
  const [practiceResult, setPracticeResult] = useState<PracticeResult | null>(null);
  const [sampleCapture, setSampleCapture] = useState<CaseResult | null>(null);
  const [sequenceResult, setSequenceResult] = useState<SequenceResult | null>(null);
  const [transformResult, setTransformResult] = useState<TransformResult | null>(null);
  const [planResult, setPlanResult] = useState<TransformPlanResult | null>(null);
  const [reductionResult, setReductionResult] = useState<ReductionResult | null>(null);
  const [reviewResult, setReviewResult] = useState<ReviewResult | null>(null);
  const [diagnosisResult, setDiagnosisResult] = useState<DiagnosisResult | null>(null);
  const [diagnosisGroupsResult, setDiagnosisGroupsResult] = useState<DiagnosisGroupsResult | null>(null);
  const [findingReviewResult, setFindingReviewResult] = useState<FindingReviewResult | null>(null);
  const [normalizeResult, setNormalizeResult] = useState<NormalizeResult | null>(null);
  const [promotionProvenance, setPromotionProvenance] = useState<PromotionProvenance | null>(null);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [environmentArtifact, setEnvironmentArtifact] = useState<{
    name: string;
    kind: "target" | "secrets" | "policy" | "reset";
  } | null>(null);

  const [restored, setRestored] = useState<RecoveryResult | null>(null);
  const [watchedRun, setWatchedRun] = useState("");
  const [drafts, setDrafts] = useState<EditorDraft[] | null>(null);
  const [capturing, setCapturing] = useState(false);
  const [importing, setImporting] = useState(false);
  const [observing, setObserving] = useState(false);
  const [captureBinding, setCaptureBinding] = useState<CaptureObservationBinding | null>(null);
  const [maintenanceTab, setMaintenanceTab] = useState<"backup" | "restore" | "storage" | "lifecycle" | "upgrade">("backup");

  // Refusals of navigation the window has not committed: the workspace and
  // case a person had stay on screen beside the reason, instead of the old
  // context being cleared before anyone knows the new one was refused.
  const [openNotice, setOpenNotice] = useState<WorkspaceResult | null>(null);
  const [caseNotice, setCaseNotice] = useState<CaseResult | null>(null);

  // A draft write that did not land. Closing the window now could lose text
  // that was never acknowledged, so closing asks first.
  const [unsavedFailure, setUnsavedFailure] = useState(false);

  const [focused, setFocused] = useState<RegionId>("commands");
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteQuery, setPaletteQuery] = useState("");
  const [theme, setTheme] = useState<Theme>("system");
  const [scale, setScale] = useState(0);
  const [split, setSplit] = useState(58);

  // Where the window is: one destination at a time and, inside some, one view.
  // Every destination stays mounted, so an unfinished edit survives looking at
  // another; only the shown one is drawn and announced.
  const [destination, setDestination] = useState<Destination>("home");
  const [caseView, setCaseView] = useState<CaseView>("messages");
  const [caseFlow, setCaseFlow] = useState<CaseFlow | null>(null);
  const [fileDetails, setFileDetails] = useState(false);
  const [testsView, setTestsView] = useState<TestsView>("suites");
  const [runsView, setRunsView] = useState<RunsView>("run");
  const [reportsView, setReportsView] = useState<ReportsView>("packets");
  const [toolsView, setToolsView] = useState<ToolsView>("inspect");
  const [settingsView, setSettingsView] = useState<SettingsView>("general");
  const [creatingProject, setCreatingProject] = useState(false);
  const [searching, setSearching] = useState(false);
  // Counts requests to open storage on a named section, so the section asked
  // for is the one shown even when storage is already open.
  const [maintenanceRequest, setMaintenanceRequest] = useState(0);

  const regionElements = useRef<Partial<Record<RegionId, HTMLElement | null>>>({});
  const searchField = useRef<HTMLInputElement | null>(null);

  const indicators = useMemo(() => {
    const table = new Map<string, Indicator>();
    for (const indicator of described?.indicators ?? []) {
      table.set(indicator.status, indicator);
    }
    return table;
  }, [described]);

  useEffect(() => {
    void (async () => {
      const result = await shell();
      setDescribed(result.shell ?? null);
      setWindowState(result.state);
      setWindowReason(result.reason);
    })();
  }, []);

  const refreshRecent = useCallback(async () => {
    setRecent(await recentWorkspaces());
  }, []);

  // The guided sample is read back out of the open folder rather than
  // remembered, so it is re-read whenever that folder or what it holds may have
  // changed. Nothing about where somebody is in it lives in this window.
  const refreshGuide = useCallback(async (folder: string | null) => {
    setGuideResult(folder ? await readGuide(folder) : null);
  }, []);

  useEffect(() => {
    void refreshRecent();
  }, [refreshRecent]);

  // The saved filters and the selected one live in the facade, not here, so the
  // view a person set up survives navigating to another case and reopening the
  // window. They are read once and after every change.
  const refreshFilters = useCallback(async () => {
    setSavedFilters(await readFilters());
  }, []);

  useEffect(() => {
    void refreshFilters();
  }, [refreshFilters]);

  // What the window restores after an interruption. It is read once, here, so
  // the panels that show it and continue from it share one answer and only one
  // of them claims the operation slot.
  const restore = useCallback(async () => {
    setRestored(await recoverSession());
  }, []);

  useEffect(() => {
    void restore();
  }, [restore]);

  // The editor drafts this viewer has retained: the work every panel of this
  // window had not stored yet. Retention does not claim the operation slot, so
  // the list is read before the panels open and is then kept current by the
  // retention outcomes themselves, each of which carries the store as it now
  // stands. A read this account cannot complete leaves the list absent and is
  // reported by the panel that asked.
  const refreshDrafts = useCallback(async () => {
    const result = await readEditorDrafts();
    setDrafts(result.drafts ?? (result.state === "empty" ? [] : null));
  }, []);

  useEffect(() => {
    void refreshDrafts();
  }, [refreshDrafts]);

  useEffect(() => {
    // A retention that landed keeps the window free to close; one that did not
    // means text was typed that was never durably acknowledged, so closing
    // asks before dropping it.
    return onRetentionResult((result, outcome) => {
      if (result.drafts) {
        setDrafts(result.drafts);
      }
      setUnsavedFailure(outcome === "save" && result.state !== "completed" && result.state !== "empty");
    });
  }, []);

  // Where this viewer is, retained by the facade so an interruption does not
  // also lose it: the open workspace, the entry selected in it, the region
  // holding focus, and the run being watched. It goes into the facade's own
  // local document, never into browser storage and never near evidence.
  // Recording it does not claim the operation slot, so it still happens while a
  // case is being verified, which is exactly when an interruption would
  // otherwise lose the most. Nothing is recorded until there is something to
  // come back to: recording an empty view as the window starts would overwrite
  // the session this same window is restoring. Recordings are chained and
  // coalesced to the newest view rather than raced, so a slow earlier record
  // can never land after a later one and leave the session remembering a place
  // the viewer has already left.
  const workspaceRoot = workspace?.workspace?.root ?? "";
  const view = useCallback((run: string) => ({
    workspace: workspaceRoot,
    region: focused,
    case: workspaceRoot === "" ? "" : (selected ?? ""),
    run,
  }), [workspaceRoot, focused, selected]);

  const recording = useRef(false);
  const pendingRecord = useRef<ReturnType<typeof view> | null>(null);
  const flushDone = useRef<Promise<void>>(Promise.resolve());
  const pushRecord = useCallback((recorded: ReturnType<typeof view>) => {
    pendingRecord.current = recorded;
    if (recording.current) {
      // A flush is already running and sends this view before it stops, so
      // waiting on it is waiting for this view to land.
      return flushDone.current;
    }
    recording.current = true;
    flushDone.current = (async () => {
      try {
        while (pendingRecord.current) {
          const next = pendingRecord.current;
          pendingRecord.current = null;
          await recordView(next);
        }
      } finally {
        recording.current = false;
      }
    })();
    return flushDone.current;
  }, []);

  useEffect(() => {
    if (workspaceRoot === "" && watchedRun === "") {
      return;
    }
    void pushRecord(view(watchedRun));
  }, [pushRecord, view, watchedRun]);

  // Naming the run folder is the one recording that has to have landed before
  // the action it describes runs, so it is awaited rather than left to the
  // effect above: a crash during a send must find the session already naming
  // the folder that holds its evidence.
  // The run panel names its output as an entry of the open workspace; the
  // session retains the folder itself, an absolute path, because that is the
  // folder recovery reads after the window is gone.
  const watch = useCallback(async (entry: string) => {
    const folder = workspaceRoot === "" ? entry : `${workspaceRoot}/${entry}`;
    setWatchedRun(folder);
    await pushRecord(view(folder));
  }, [pushRecord, view, workspaceRoot]);

  // The chosen theme and text size are applied to the document and written
  // nowhere: both follow the system again the next time the window opens.
  useEffect(() => {
    if (theme === "system") {
      document.documentElement.removeAttribute("data-theme");
    } else {
      document.documentElement.setAttribute("data-theme", theme);
    }
  }, [theme]);

  useEffect(() => {
    const percent = described?.text_scales[scale] ?? 100;
    document.documentElement.style.setProperty("--text-scale", String(percent / 100));
  }, [described, scale]);

  // Whether an operation holds the window's one slot: this window's own, or a
  // screen's whose call holds the facade, such as the raw inspection and the
  // performance corpus.
  const busy = useWindowBusy();
  const root = workspace?.workspace?.root ?? null;
  const opened = workspace?.workspace;
  const overview = investigation?.overview ?? null;
  const verified = evidence?.case ?? null;

  const focusRegion = useCallback((region: RegionId) => {
    regionElements.current[region]?.focus();
  }, []);

  // F6 moves through the regions shown now: message details is a region only
  // while it is open, so a step never lands on a pane that is not there.
  const step = useCallback(
    (direction: number) => {
      const regions = (described?.regions ?? []).filter((region) => {
        const element = regionElements.current[region.id];
        return element && !element.hidden;
      });
      if (regions.length === 0) {
        return;
      }
      const current = regions.findIndex((region) => region.id === focused);
      const next = regions[(current + direction + regions.length) % regions.length];
      if (next) {
        focusRegion(next.id);
      }
    },
    [described, focused, focusRegion],
  );

  /** Everything derived from the one verified case lives only while that case
   * is displayed, so one seam clears it when the window's case changes. A
   * panel whose results derive from the open case registers its setter here
   * and nowhere else. */
  const clearCase = useCallback(() => {
    setEvidence(null);
    setGridResult(null);
    setIndexResult(null);
    setInspectionResult(null);
    setReproducerResult(null);
    setTestResult(null);
    setComparisonResult(null);
    setSequenceResult(null);
    setRevisionResult(null);
    setComparisonSeed(null);
    setTransformResult(null);
    setPlanResult(null);
    setReductionResult(null);
    setReviewResult(null);
    setDiagnosisResult(null);
    setDiagnosisGroupsResult(null);
    setFindingReviewResult(null);
    setNormalizeResult(null);
    setPromotionProvenance(null);
    setSelectedOccurrence(null);
  }, []);

  /** Opening a folder changes the workspace, so everything the previous
   * workspace derived is cleared alongside the case-derived state. */
  const clearWorkspace = useCallback(() => {
    clearCase();
    setInvestigation(null);
    setEditable(null);
    setFound(null);
  }, [clearCase]);

  // Opening a folder is a navigation, and a navigation is committed only once
  // it is accepted. A dismissed dialog, a folder that cannot be opened or an
  // account that cannot read it leaves the investigation exactly as it was,
  // with the refusal shown beside it; nothing here clears first, so no
  // cancellation can silently drop the open workspace, its case or an edit.
  const openFolder = useCallback(
    async (operation: () => Promise<WorkspaceResult>) => {
      setOpenNotice(null);
      let opened = false;
      await run("workspace", async () => {
        const result = await operation();
        if (result.workspace) {
          clearWorkspace();
          setSelected(null);
          setWorkspace(result);
          setPracticeResult(null);
          setSampleCapture(null);
          setImporting(false);
          setCapturing(false);
          setObserving(false);
          setCaptureBinding(null);
          opened = true;
          await refreshGuide(result.workspace.root);
          // A folder that holds a project opens as that project: its cases and
          // settings are read now rather than behind another button.
          if (result.workspace.artifacts.some((artifact) => artifact.kind === "project")) {
            setInvestigation(await openProjectOverview(result.workspace.root));
          }
        } else {
          setOpenNotice(result);
        }
      });
      await refreshRecent();
      if (opened) {
        setDestination("cases");
        focusRegion("evidence");
      }
      return opened;
    },
    [clearWorkspace, focusRegion, refreshGuide, refreshRecent, run],
  );

  // Forgetting a recent folder writes the list, so it takes its turn like
  // every other write; what the list then holds is what the facade answers,
  // including when it refused.
  const forget = useCallback(
    async (folder: string) => {
      await run("recent", async () => {
        setRecent(await forgetWorkspace(folder));
      });
    },
    [run],
  );

  // One window of the grid at a time. Asking for the next one re-reads the case
  // and its index, so a window is never served from an index the evidence no
  // longer supports. What the grid says about its index comes from that same
  // read, so a page verifies the case once and the index details beside it can
  // never describe another reading of the case.
  const showGrid = useCallback(
    async (folder: string, name: string, indexName: string, offset: number) => {
      if (!bounds) return;
      // A read whose answer can be older than the question: only the answer
      // to the latest request commits, so a late answer to an earlier one is
      // dropped instead of overwriting what the viewer asked for last.
      await run("grid", async (current) => {
        setGridResult(null);
        setInspectionResult(null);
        setSelectedOccurrence(null);
        const result = await openGrid(folder, name, indexName, offset, bounds.grid);
        if (current()) {
          setGridResult(result);
          setIndexResult(result.index ? { state: result.state, index: result.index } : null);
        }
      });
    },
    [bounds, run],
  );

  const handleBuildIndex = useCallback(
    async (request: BuildIndexRequest) => {
      if (!root) return;
      let builtIndexName: string | null = null;
      await run("grid", async () => {
        request.workspace = root;
        const res = await buildIndex(request);
        setIndexResult(res);
        if (res.state === "completed" && res.index) {
          builtIndexName = res.index.index_name;
          await openWorkspace(root);
        }
      });
      if (builtIndexName) {
        void showGrid(root, request.case, builtIndexName, 0);
      }
    },
    [root, run, showGrid],
  );

  // Verifying a case is the same kind of committed navigation. The case and
  // everything derived from it stay while the new one is read — the progress
  // line says that a read is running — and they stay, marked stale by the
  // refusal beside them, when the new one is refused.
  const verifyCase = useCallback(
    async (folder: string, name: string, options?: { skipAutoGrid?: boolean }): Promise<CaseResult | null> => {
      setCaseNotice(null);
      let autoIndex: string | null = null;
      let outcome: CaseResult | null = null;
      await run("case", async () => {
        const result = await openCase(folder, name);
        if (result.case) {
          clearCase();
          setSelected(name);
          setEvidence(result);
          setDestination("cases");
          setCaseView("messages");
          setCaseFlow(null);
          outcome = result;
          const desc = await describeIndex(folder, name, "");
          setIndexResult(desc);
          if (desc.state === "completed" && desc.index?.applicable) {
            autoIndex = desc.index.index_name;
          }
        } else {
          setCaseNotice(result);
        }
      });
      if (autoIndex && !options?.skipAutoGrid) {
        void showGrid(folder, name, autoIndex, 0);
      }
      focusRegion("evidence");
      return outcome;
    },
    [clearCase, focusRegion, run, showGrid],
  );

  // An occurrence is selected from the grid or from the sequence, and both name
  // the same verified case: the grid's own case when there is one, and the case
  // the window verified otherwise. Either way the inspector is bound to the
  // identity that was displayed, so a value is never read out of evidence that
  // has changed since.
  const inspect = useCallback(
    async (
      occurrence: string,
      path: string,
      nodeOffset: number,
      byteOffset: number,
      targetCase?: { case: string; identity: string },
    ) => {
      const grid = gridResult?.grid;
      const open =
        targetCase ??
        (grid
          ? { case: grid.case, identity: grid.identity }
          : evidence?.case
            ? { case: evidence.case.name, identity: evidence.case.identity }
            : null);
      if (!root || !open) return;
      await run("inspect", async (current) => {
        setSelectedOccurrence(occurrence);
        setInspectionResult(null);
        const result = await inspectOccurrence({
          workspace: root,
          case: open.case,
          identity: open.identity,
          occurrence,
          path,
          node_offset: nodeOffset,
          byte_offset: byteOffset,
        });
        if (current()) {
          setInspectionResult(result);
        }
      });
    },
    [evidence, gridResult, root, run],
  );

  // One practice run of the guided sample. It is the one operation in this
  // window that sends, so Cancel is offered while it runs, and what the folder
  // then holds is read back rather than assumed from the result.
  const practise = useCallback(
    async (trial: GuideTrialId, output: string) => {
      if (!root || !guideResult?.guide?.spec) return;
      const spec = guideResult.guide.spec;
      await run("practice", async () => {
        setPracticeResult(null);
        setPracticeResult(await runPractice({ workspace: root, spec, trial, output }));
      });
      await refreshGuide(root);
    },
    [guideResult, refreshGuide, root, run],
  );

  // A suite the suite panel prepared for execution is handed to the durable-run
  // panel by seeding its selection with the suite entry and the environment
  // it was prepared against, so the execution center's own preflight and
  // explicit send decision take over without a path being copied by hand. The
  // prepared folder and release pins are only named beside it: the run
  // preflight takes neither.
  const handoff = useCallback(
    (selection: SuiteRunHandoff) => {
      setRunSpecPath(selection.entry);
      setRunEnvironment(selection.environment);
      if (root) setSuiteHandoff({ workspace: root, handoff: selection });
      setDestination("runs");
      setRunsView("run");
      focusRegion("evidence");
    },
    [focusRegion, root],
  );

  // The project overview re-reads the project from disk every time: what the
  // window shows is what the project records, never what an earlier action
  // reported. Every write below returns the refreshed overview, so a panel is
  // never left beside state the project has moved past.
  const readProject = useCallback(
    async (folder: string) => {
      await run("project", async () => {
        setInvestigation(null);
        setInvestigation(await openProjectOverview(folder));
      });
      setDestination("cases");
      focusRegion("evidence");
    },
    [focusRegion, run],
  );

  // A new project is a new folder inside the one chosen for it. The window
  // moves into that folder, as opening it would, so every later project
  // action — registering a case, importing evidence, opening what was
  // imported — reads and writes the project just created rather than the
  // folder around it; what was open in the folder it leaves is closed. If the
  // new folder cannot be opened, its refusal is shown and no project stays
  // open over the wrong folder.
  const startProject = useCallback(
    async (name: string, title: string, owner: string, versions: string[]) => {
      let created = "";
      let answer: ProjectOverviewResult = { state: "failed", reason: "The project was not created." };
      await run("project", async () => {
        const result = await createProject(name, title, owner, versions);
        answer = result;
        setInvestigation(result);
        if (result.overview) created = result.overview.root;
      });
      if (created === "") return answer;
      if (created === root) {
        setDestination("cases");
        return answer;
      }
      if (await openFolder(() => openWorkspace(created))) {
        await readProject(created);
      } else {
        setInvestigation(null);
      }
      return answer;
    },
    [openFolder, readProject, root, run],
  );

  // A refused project write — a license that no longer admits it, a change
  // the project cannot take — answers with its reason and no overview. The
  // window keeps the project as it last read it beside that refusal, so a
  // refusal never takes the project and its controls off the screen.
  const settleProject = useCallback((answer: ProjectOverviewResult) => {
    setInvestigation((held) => (answer.overview || !held?.overview ? answer : { ...answer, overview: held.overview }));
  }, []);

  // Whether the change was stored, so the form keeps what was typed beside a
  // refusal and clears only what the project took.
  const editSettings = useCallback(
    async (change: SettingsChange) => {
      if (!root) return false;
      let stored = false;
      await run("project", async () => {
        const answer = await updateProjectSettings(root, change);
        settleProject(answer);
        stored = answer.state === "completed";
      });
      return stored;
    },
    [root, run, settleProject],
  );

  // The editable document is read from disk each time the overview is asked
  // to show it, never kept from an earlier read.
  const readEditable = useCallback(async () => {
    if (!root) return;
    await run("project", async () => {
      setEditable(null);
      setEditable(await openRevisions(root));
    });
  }, [root, run]);

  const register = useCallback(
    async (name: string, registration: CaseRegistration) => {
      if (!root) return;
      await run("project", async () => {
        settleProject(await registerCase(root, name, registration));
      });
    },
    [root, run, settleProject],
  );

  const updateCase = useCallback(
    async (name: string, change: CaseChange) => {
      if (!root) return;
      await run("project", async () => {
        settleProject(await updateRegisteredCase(root, name, change));
      });
    },
    [root, run, settleProject],
  );

  // A search result opens the thing it found, the way the window already
  // opens it: a registered case or a case entry is verified and opened, the
  // project documents open the project. A result that names an entry this
  // window does not open still takes the person to its region. For indexed
  // content matches, it verifies the case, inspects the occurrence, and focuses
  // the inspector without requiring an already opened grid.
  const openMatch = useCallback(
    (match: Match) => {
      if (!root || busy) return;
      if (match.kind === "content") {
        void (async () => {
          const verified = await verifyCase(root, match.name, { skipAutoGrid: true });
          if (match.occurrence && verified?.case) {
            await inspect(match.occurrence, match.selector ?? "", 0, -1, {
              case: verified.case.name,
              identity: verified.case.identity,
            });
            focusRegion("inspector");
          }
        })();
        return;
      }
      if (match.kind === "registered_case") {
        void verifyCase(root, match.name);
        return;
      }
      const artifact = opened?.artifacts.find((entry) => entry.name === match.name);
      if (artifact?.kind === "case") {
        void verifyCase(root, match.name);
        return;
      }
      if (artifact?.kind === "project" || artifact?.kind === "revisions") {
        void readProject(root);
        return;
      }
      setDestination("cases");
      focusRegion(match.region);
    },
    [busy, focusRegion, inspect, opened, readProject, root, verifyCase],
  );

  // Breadcrumb back navigation: out of the case to the project, and out of
  // the project to the folder listing. The trail is the way out as well as
  // the way in.
  const backToProject = useCallback(() => {
    setImporting(false);
    setCapturing(false);
    setObserving(false);
    setCaptureBinding(null);
    clearCase();
    setCaseFlow(null);
    setDestination("cases");
    focusRegion("evidence");
  }, [clearCase, focusRegion]);

  const searchWorkspace = useCallback(
    async (folder: string, wanted: string) => {
      await run("search", async () => {
        setFound(null);
        setFound(await search(folder, wanted));
      });
    },
    [run],
  );

  // A comparison is bound to the identity the window verified for the open
  // case, so one is never shown beside counts from evidence that has changed.
  // Asking for the next window is another comparison: both collections are
  // read and aligned again rather than a row list being held here. A
  // normalization reading stays below it only while it reads the comparison
  // shown: a refused comparison, or one of another pair, withdraws it.
  const compareCollections = useCallback(
    async (right: string, keys: string[], fields: string[], offset: number) => {
      const open = evidence?.case;
      if (!root || !open || !bounds) return;
      await run("comparison", async () => {
        setComparisonResult(null);
        const compared = await compare({
          workspace: root,
          left: open.name,
          identity: open.identity,
          right,
          keys,
          fields,
          offset,
          limit: bounds.comparison,
        });
        setComparisonResult(compared);
        setNormalizeResult((reading) =>
          readsComparison(reading?.normalization, compared.comparison) ? reading : null,
        );
      });
    },
    [bounds, evidence, root, run],
  );

  // A save that wrote a new workspace entry changes what the folder lists, so
  // the listing is read again and the pickers offer the new revision. The open
  // case and everything derived from it stay exactly as they are.
  const refreshListing = useCallback(async () => {
    if (!root) return;
    const result = await openWorkspace(root);
    if (result.workspace) {
      setWorkspace(result);
    }
  }, [root]);

  // The guided sample's import of the frozen receiver fixtures writes one new
  // entry of the open folder, so the listing is read again once it has.
  const importSample = useCallback(
    async (output: string) => {
      if (!root) return;
      let written = false;
      await run("sample", async () => {
        setSampleCapture(null);
        const result = await captureSample({ workspace: root, output });
        setSampleCapture(result);
        written = result.state === "completed";
      });
      if (written) await refreshListing();
    },
    [refreshListing, root, run],
  );

  // An import can register the case it wrote into the open project, and it
  // writes the case and its receipt as new entries of the open folder.
  // Leaving the import panel re-reads both, so neither the overview nor any
  // panel that offers the folder's entries lists less than is now there.
  const leaveImport = useCallback(async () => {
    const project = investigation?.overview?.root;
    if (project) await readProject(project);
    await refreshListing();
  }, [investigation, readProject, refreshListing]);

  // A diagnosis is bound to the identity the window verified for the open
  // case, and it writes one new report directory, exactly as `readmit
  // diagnose` writes it, so the listing is read again once it lands.
  const diagnose = useCallback(
    async (request: DiagnosisRequest) => {
      const open = evidence?.case;
      if (!root || !open) return;
      await run("diagnosis", async () => {
        setDiagnosisResult(null);
        setFindingReviewResult(null);
        setDiagnosisResult(
          await runDiagnosis({ ...request, workspace: root, case: open.name, identity: open.identity }),
        );
      });
      await refreshListing();
    },
    [evidence, refreshListing, root, run],
  );

  // A retained report is read again on every call, including for the next
  // window of its findings, with the same strict reader a review uses.
  const openReport = useCallback(
    async (entry: string, offset: number) => {
      if (!root) return;
      await run("diagnosis-report", async () => {
        setDiagnosisResult(null);
        setFindingReviewResult(null);
        setDiagnosisResult(await openDiagnosisReport(root, entry, offset));
      });
    },
    [root, run],
  );

  const groupCases = useCallback(
    async (request: GroupDiagnosesRequest) => {
      if (!root) return;
      await run("diagnosis-groups", async () => {
        setDiagnosisGroupsResult(null);
        setDiagnosisGroupsResult(await groupDiagnoses({ ...request, workspace: root }));
      });
    },
    [root, run],
  );

  const openGroupsReport = useCallback(
    async (entry: string, offset: number) => {
      if (!root) return;
      await run("diagnosis-groups-report", async () => {
        setDiagnosisGroupsResult(null);
        setDiagnosisGroupsResult(await openDiagnosisGroupsReport(root, entry, offset));
      });
    },
    [root, run],
  );

  // A finding review joins the displayed diagnosis and the analyst's typed
  // decisions. Previewing writes nothing; deciding persists the decisions
  // document and the review directory and the listing is read again.
  const reviewDiagnosisFindings = useCallback(
    async (request: FindingReviewRequest, write: boolean) => {
      const open = evidence?.case;
      if (!root || !open) return;
      await run("finding-review", async () => {
        setFindingReviewResult(null);
        setFindingReviewResult(
          await (write ? decideFindings : reviewFindings)({
            ...request,
            workspace: root,
            case: open.name,
            identity: open.identity,
          }),
        );
      });
      if (write) {
        await refreshListing();
      }
    },
    [evidence, refreshListing, root, run],
  );

  // A policy-scoped reading of the same comparison. It never edits the raw
  // comparison, which stays displayed above it, and never changes a source
  // byte; the policy is read again on every call.
  const normalizeCollections = useCallback(
    async (right: string, policy: string, keys: string[], fields: string[], offset: number) => {
      const open = evidence?.case;
      if (!root || !open || !bounds) return;
      await run("normalize", async () => {
        setNormalizeResult(null);
        setNormalizeResult(
          await normalizeCompare({
            workspace: root,
            left: open.name,
            identity: open.identity,
            right,
            policy,
            keys,
            fields,
            offset,
            limit: bounds.comparison,
          }),
        );
      });
    },
    [bounds, evidence, root, run],
  );

  // A sequence is bound to the identity the window verified for the open case,
  // so one is never drawn beside counts from evidence that has changed. Asking
  // for the next window is another sequence: the case is verified and the rules
  // are read again rather than an event list being held here.
  const layOutSequence = useCallback(
    async (rules: string, offset: number, analysis = "") => {
      const open = evidence?.case;
      if (!root || !open || !bounds) return;
      await run("sequence", async () => {
        setSequenceResult(null);
        setSequenceResult(
          await openSequence({
            workspace: root,
            case: open.name,
            identity: open.identity,
            rules,
            analysis,
            offset,
            limit: bounds.sequence,
          }),
        );
      });
    },
    [bounds, evidence, root, run],
  );

  // Comparing two built revisions reads two finished reproducers and the runs
  // retained for them. It is bound to nothing the window is holding: both are
  // named entries of the open workspace and are verified again on every call.
  const compareRevisions = useCallback(
    async (left: string, right: string, leftResult: string, rightResult: string) => {
      if (!root) return;
      await run("revisions", async () => {
        setRevisionResult(null);
        setRevisionResult(
          await compareReproducers({
            workspace: root,
            left,
            right,
            left_result: leftResult,
            right_result: rightResult,
          }),
        );
      });
    },
    [root, run],
  );

  // A transformation preview is bound to the identity the window verified for
  // the open case, so a plan is never previewed against evidence that changed
  // since. It writes nothing at all: the documents are read again on every call
  // and no preview is held here.
  const previewPlan = useCallback(
    async (rules: string, plan: string, profile: string) => {
      const open = evidence?.case;
      if (!root || !open) return;
      await run("transformation", async () => {
        setTransformResult(null);
        setTransformResult(
          await previewTransformation({
            workspace: root,
            case: open.name,
            identity: open.identity,
            rules,
            plan,
            profile,
          }),
        );
      });
    },
    [evidence, root, run],
  );

  // A saved plan is a new entry of the folder, so the listing is read again
  // before the panel is answered and its pickers offer the plan at once.
  const savePlan = useCallback(
    async (rules: string, profile: string, steps: TransformStep[], output: string) => {
      const open = evidence?.case;
      if (!root || !open) return null;
      const answered: { result: TransformPlanResult | null } = { result: null };
      await run("transform-plan", async () => {
        setPlanResult(null);
        const result = await saveTransformPlan({
          workspace: root,
          case: open.name,
          identity: open.identity,
          rules,
          profile,
          steps,
          output,
        });
        setPlanResult(result);
        answered.result = result;
        if (result.plan) {
          const refreshed = await openWorkspace(root);
          if (refreshed.workspace) setWorkspace(refreshed);
        }
      });
      return answered.result;
    },
    [evidence, root, run],
  );

  // Reopening a plan reads the entry again through the decoder a preview
  // uses and writes nothing; the panel takes its steps from the answer.
  const openPlan = useCallback(
    async (entry: string) => {
      if (!root || !evidence?.case) return null;
      const answered: { result: TransformPlanResult | null } = { result: null };
      await run("transform-open", async () => {
        setPlanResult(null);
        const result = await openTransformPlan(root, entry);
        setPlanResult(result);
        answered.result = result;
      });
      return answered.result;
    },
    [evidence, root, run],
  );

  const runReductionPreview = useCallback(
    async (config: ReductionForm) => {
      const open = evidence?.case;
      if (!root || !open) return;
      await run("reduction-preview", async () => {
        setReductionResult(null);
        setReductionResult(
          await previewReduction({
            workspace: root,
            case: open.name,
            identity: open.identity,
            spec: config.spec,
            rules: config.rules,
            grouping: config.grouping,
            assertions: config.assertions,
            trials: config.trials,
            confirmations: config.confirmations,
            reset_plan: config.resetPlan,
            target: config.target,
            policy: config.policy,
            confirmed: config.confirmed,
            work: config.work,
          }),
        );
      });
    },
    [evidence, root, run],
  );

  const runReduction = useCallback(
    async (config: ReductionForm) => {
      const open = evidence?.case;
      if (!root || !open) return;
      await run("reduction", async () => {
        setReductionResult(null);
        setReductionResult(
          await startReduction({
            workspace: root,
            case: open.name,
            identity: open.identity,
            spec: config.spec,
            rules: config.rules,
            grouping: config.grouping,
            assertions: config.assertions,
            trials: config.trials,
            confirmations: config.confirmations,
            reset_plan: config.resetPlan,
            target: config.target,
            policy: config.policy,
            confirmed: config.confirmed,
            work: config.work,
          }),
        );
      });
    },
    [evidence, root, run],
  );

  // The project's answer goes back to the reproducer panel as well as to the
  // overview, so a refused registration is reported beside the build it was
  // for rather than read there as registered.
  const registerBuiltRevision = useCallback(
    async (source: string, name: string, parent: string) => {
      if (!root) return null;
      const answered: { result: ProjectOverviewResult | null } = { result: null };
      await run("project", async () => {
        const result = await registerRevision({
          workspace: root,
          source,
          name,
          parent,
        });
        answered.result = result;
        settleProject(result);
        setWorkspace(await openWorkspace(root));
      });
      return answered.result;
    },
    [root, run, settleProject],
  );

  // A build handed to the revision comparison becomes its later revision; the
  // comparison on screen belonged to other revisions, so it is withdrawn.
  const compareBuild = useCallback((built: string) => {
    setCaseFlow("compare");
    setRevisionResult(null);
    setComparisonSeed((held) => ({ later: built, serial: (held?.serial ?? 0) + 1 }));
  }, []);


  // An export review is read again on every call, including for the next window
  // of its inventory, so the identity an approval names is always the one the
  // bytes on disk have now. The approval a person typed is sent and checked; it
  // is never retained here or anywhere else.
  const readReview = useCallback(
    async (review: string, approve: string, offset: number) => {
      if (!root || !bounds) return;
      await run("review", async () => {
        setReviewResult(null);
        setReviewResult(
          await openReview({
            workspace: root,
            review,
            approve,
            offset,
            limit: bounds.review,
          }),
        );
      });
    },
    [bounds, root, run],
  );

  // A reproducer plan is bound to the case the grid verified, so every step
  // carries the plan back to the engine, which decides what it means. The
  // window keeps no second copy of the selection or of what a dependency added.
  // A plan the engine accepted is retained as an unstored editor draft under an
  // internal identity, so the editing survives an interruption; a build writes
  // the real manifest beside the evidence and drops the draft.
  const reproducerDraftId = useRef("");
  const testDraftId = useRef("");
  const promotedDraftId = useRef("");

  const keepReproducerDraft = useCallback(
    async (plan: ReproducerPlan, open: { case: string; identity: string }) => {
      const result = await saveEditorDraft({
        id: reproducerDraftId.current,
        kind: "reproducer-plan",
        workspace: root ?? "",
        case: open.case,
        identity: open.identity,
        content_schema: "readmit-reproducer-plan/v1",
        content: plan,
      });
      if (result.state === "completed" || result.state === "empty") {
        if (reproducerDraftId.current === "") {
          reproducerDraftId.current = savedId(result, "reproducer-plan", root ?? "");
        }
      }
    },
    [root],
  );

  const dropReproducerDraft = useCallback(async () => {
    if (reproducerDraftId.current !== "") {
      await discardEditorDraft(reproducerDraftId.current);
    }
    reproducerDraftId.current = "";
  }, []);

  const reproduce = useCallback(
    async (work: (plan: ReproducerPlan, open: { case: string; identity: string }) => Promise<ReproducerResult>) => {
      const open = gridResult?.grid;
      if (!root || !open) return;
      const plan = reproducerResult?.reproducer?.plan ?? ({ schema: "", case: "", steps: [] } as ReproducerPlan);
      await run("reproducer", async () => {
        const result = await work(plan, open);
        // A refused step leaves the plan exactly as it was, so the refusal is
        // shown without replacing the reproducer a person is working on.
        const kept = reproducerResult?.reproducer;
        setReproducerResult(
          result.reproducer || !kept ? result : { ...result, reproducer: kept },
        );
        if (result.reproducer) {
          if (result.reproducer.output) {
            await dropReproducerDraft();
          } else {
            await keepReproducerDraft(result.reproducer.plan, open);
          }
        }
      });
    },
    [dropReproducerDraft, gridResult, keepReproducerDraft, reproducerResult, root, run],
  );

  // A test draft is bound to the case the grid verified, so every answer
  // carries the draft back to the engine, which decides what it now means. The
  // window keeps no second copy of the answers and holds the draft nowhere
  // else: it is unstored work, and it is never placed in browser storage. A
  // draft the engine accepted is retained under an internal identity, so the
  // authoring survives an interruption; saving the spec writes the real
  // document beside the evidence and drops the draft.
  const author = useCallback(
    async (work: (draft: TestDraftDocument, open: { case: string; identity: string }) => Promise<TestResult>) => {
      const open = gridResult?.grid;
      if (!root || !open) return;
      const draft =
        testResult?.test?.draft ??
        ({
          schema: "",
          case: { entry: "", identity: "" },
          name: "",
          messages: [],
          target: "",
          boundary: "",
          observation: "",
          reset: "",
          expectations: [],
        } as TestDraftDocument);
      await run("authoring", async () => {
        const result = await work(draft, open);
        // A refused answer leaves the draft exactly as it was, so the refusal is
        // shown without replacing the test a person is working on.
        const kept = testResult?.test;
        setTestResult(result.test || !kept ? result : { ...result, test: kept });
        if (result.test) {
          if (result.test.output) {
            if (testDraftId.current !== "") {
              await discardEditorDraft(testDraftId.current);
            }
            testDraftId.current = "";
            if (promotedDraftId.current !== "") {
              await discardEditorDraft(promotedDraftId.current);
            }
            promotedDraftId.current = "";
            setPromotionProvenance(null);
          } else {
            const retained = await saveEditorDraft({
              id: testDraftId.current,
              kind: "test-draft",
              workspace: root,
              case: open.case,
              identity: open.identity,
              content_schema: "readmit-test-draft/v1",
              content: result.test.draft,
            });
            if ((retained.state === "completed" || retained.state === "empty") && testDraftId.current === "") {
              testDraftId.current = savedId(retained, "test-draft", root);
            }
          }
        }
        // A saved spec is a step of the guided sample, and an entry the run
        // panel offers, so what the folder now holds is read again rather than
        // inferred from this call succeeding.
        if (result.test?.output) {
          setRunSpecPath(result.test.output);
          setRunEnvironment(undefined);
          setSuiteHandoff(null);
          await refreshGuide(root);
          await refreshListing();
        }
      });
    },
    [gridResult, refreshGuide, refreshListing, root, run, testResult],
  );

  // Promoting one explicitly confirmed finding answers the existing authoring
  // flow with exactly what the review engine derived from the verified case:
  // the fixed acknowledgement boundary, the messages and the expectations. The
  // engine re-validates every answer, so re-answering them cannot silently
  // weaken anything, and where it came from rides beside the draft as
  // provenance — in the editor-draft envelope, never inside the spec.
  const promoteFinding = useCallback(
    async (status: FindingStatus, reviewEntry: string, reportSHA256: string) => {
      const promotion = status.promotion;
      const open = evidence?.case;
      if (!promotion || !root || !open) return;
      await run("authoring", async () => {
        const base = { workspace: root, case: open.name, identity: open.identity };
        const emptyDraft: TestDraftDocument = {
          schema: "",
          case: { entry: "", identity: "" },
          name: "",
          messages: [],
          target: "",
          boundary: "",
          observation: "",
          reset: "",
          expectations: [],
        };
        // A diagnosis promotion is drafted at the ack-contract boundary only;
        // the review states that boundary and the authoring flow accepts no other.
        let result = await authorTest({
          ...base,
          draft: emptyDraft,
          answer: { stage: "boundary", boundary: "ack-contract" },
        });
        if (result.state !== "completed" || !result.test) {
          setTestResult(result);
          return;
        }
        result = await authorTest({
          ...base,
          draft: result.test.draft,
          answer: { stage: "messages", messages: promotion.messages },
        });
        if (result.state !== "completed" || !result.test) {
          setTestResult(result);
          return;
        }
        result = await authorTest({
          ...base,
          draft: result.test.draft,
          answer: { stage: "expectations", expectations: promotion.expectations },
        });
        setTestResult(result);
        if (result.state !== "completed" || !result.test) {
          return;
        }
        const provenance: PromotionProvenance = {
          finding: status.finding,
          report_sha256: reportSHA256,
          review: reviewEntry,
          decision_rationale: status.rationale ?? "",
        };
        setPromotionProvenance(provenance);
        const retained = await saveEditorDraft({
          id: promotedDraftId.current,
          kind: "promoted-test-draft",
          workspace: root,
          case: open.name,
          identity: open.identity,
          content_schema: "readmit-promoted-test-draft/v1",
          content: { schema: "readmit-promoted-test-draft/v1", draft: result.test.draft, provenance },
        });
        if (
          (retained.state === "completed" || retained.state === "empty") &&
          promotedDraftId.current === ""
        ) {
          promotedDraftId.current = savedId(retained, "promoted-test-draft", root);
        }
      });
      setCaseFlow("test");
      focusRegion("evidence");
    },
    [evidence, findingReviewResult, focusRegion, root, run],
  );

  // A test draft this viewer had not stored comes back only when it names the
  // case the window has verified now, identity and all: a draft authored
  // against evidence that has changed or moved is offered as what it is —
  // stale work this window does not silently rebind.
  useEffect(() => {
    const grid = gridResult?.grid;
    if (!grid || !root || testResult !== null) {
      return;
    }
    const held = drafts?.find(
      (entry) =>
        entry.kind === "test-draft" &&
        entry.workspace === root &&
        entry.case === grid.case &&
        entry.identity === grid.identity,
    );
    if (!held) {
      return;
    }
    testDraftId.current = held.id;
    setTestResult({
      state: "completed",
      test: { draft: held.content as TestDraftDocument },
    });
  }, [drafts, gridResult, root, testResult]);

  // The reproducer plan comes back the same way, and under the same rule.
  useEffect(() => {
    const grid = gridResult?.grid;
    if (!grid || !root || reproducerResult !== null) {
      return;
    }
    const held = drafts?.find(
      (entry) =>
        entry.kind === "reproducer-plan" &&
        entry.workspace === root &&
        entry.case === grid.case &&
        entry.identity === grid.identity,
    );
    if (!held) {
      return;
    }
    reproducerDraftId.current = held.id;
    setReproducerResult({
      state: "completed",
      reproducer: { plan: held.content as ReproducerPlan },
    });
  }, [drafts, gridResult, reproducerResult, root]);

  // A promoted test draft comes back the same way, provenance and all, so a
  // draft that came from a confirmed finding never loses where it came from.
  useEffect(() => {
    const grid = gridResult?.grid;
    if (!grid || !root || testResult !== null) {
      return;
    }
    const held = drafts?.find(
      (entry) =>
        entry.kind === "promoted-test-draft" &&
        entry.workspace === root &&
        entry.case === grid.case &&
        entry.identity === grid.identity,
    );
    if (!held) {
      return;
    }
    promotedDraftId.current = held.id;
    const content = held.content as { draft: TestDraftDocument; provenance: PromotionProvenance };
    setTestResult({ state: "completed", test: { draft: content.draft } });
    setPromotionProvenance(content.provenance);
  }, [drafts, gridResult, root, testResult]);

  // Drops one retained editor draft and takes it out of the local list at the
  // same moment, so a panel cannot offer the same draft back again while the
  // facade's answer is still in flight.
  const dropDraft = useCallback((id: string) => {
    setDrafts((current) => current?.filter((draft) => draft.id !== id) ?? null);
    void discardEditorDraft(id);
  }, []);

  const discardTestDraft = useCallback(() => {
    if (testDraftId.current !== "") {
      dropDraft(testDraftId.current);
    }
    testDraftId.current = "";
    if (promotedDraftId.current !== "") {
      dropDraft(promotedDraftId.current);
    }
    promotedDraftId.current = "";
    setPromotionProvenance(null);
    setTestResult(null);
  }, [dropDraft]);

  const discardReproducerDraft = useCallback(() => {
    if (reproducerDraftId.current !== "") {
      dropDraft(reproducerDraftId.current);
    }
    reproducerDraftId.current = "";
    setReproducerResult(null);
  }, [dropDraft]);

  // Reopening where a person was is their own decision, made by pressing the
  // one button that offers it; nothing restores a workspace by itself. The
  // restore is read-only — it opens folders, verifies evidence and moves
  // focus, and it never resumes or resends anything — and where the retained
  // artifacts have moved or changed, the ordinary refusals are shown and the
  // listing is there to reopen from, so a draft is never bound to different
  // evidence silently.
  const reopen = useCallback(async () => {
    const where = restored?.session?.view;
    if (!where?.workspace) {
      return;
    }
    await openFolder(() => openWorkspace(where.workspace));
    if (where.case) {
      await verifyCase(where.workspace, where.case);
    }
    if (where.region) {
      focusRegion(where.region as RegionId);
    }
  }, [focusRegion, openFolder, restored, verifyCase]);

  // Closing the window is safe while every edit is durably acknowledged. After
  // a retention the facade refused, there is text that was typed and never
  // acknowledged, so closing asks first instead of losing it quietly.
  useEffect(() => {
    if (!unsavedFailure) {
      return;
    }
    const guard = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [unsavedFailure]);

  // Changing the filter changes what the open grid is showing, so the window is
  // rendered again from its first page rather than left showing the old view.
  const changeFilters = useCallback(
    async (work: () => Promise<FiltersResult>) => {
      let stored = false;
      await run("filters", async () => {
        const result = await work();
        setSavedFilters(result);
        stored = result.state === "completed" || result.state === "empty";
      });
      // A change that was refused left the selection as it was, so the open
      // window is still the right one: rendering it again would only replace a
      // correct view with an identical one and hide the refusal behind it.
      const open = gridResult?.grid;
      if (stored && root && open) {
        await showGrid(root, open.case, open.index, 0);
      }
    },
    [gridResult, root, run, showGrid],
  );


  // Moves to a destination and, where it has views, to one of them. Focus goes
  // to the page, so a keyboard user lands where the page now is.
  const go = useCallback(
    (to: Destination) => {
      setDestination(to);
      focusRegion("evidence");
    },
    [focusRegion],
  );

  const openSettings = useCallback(
    (view: SettingsView) => {
      setSettingsView(view);
      go("settings");
    },
    [go],
  );

  const openStorage = useCallback(
    (tab: "backup" | "restore" | "storage" | "lifecycle" | "upgrade") => {
      setMaintenanceTab(tab);
      setMaintenanceRequest((count) => count + 1);
      openSettings("storage");
    },
    [openSettings],
  );

  // Every command the facade declares has an action here. The record is keyed by
  // the declared identifiers, so a command the window forgot is a type error
  // rather than a palette entry that quietly does nothing. Which key reaches
  // which command is not typed, and is what the facade's own tests check.
  const actions: Record<CommandId, () => void> = {
    "command-palette": () => {
      setPaletteQuery("");
      setPaletteOpen(true);
    },
    "search-workspace": () => {
      if (root) setSearching(true);
    },
    "open-workspace": () => {
      if (!busy) {
        void openFolder(selectWorkspace);
      }
    },
    "create-sample-workspace": () => {
      if (!busy) {
        void openFolder(createSampleWorkspace);
      }
    },
    "open-project": () => {
      if (!busy && root) {
        void readProject(root);
      }
    },
    "manage-profiles": () => {
      setTestsView("profiles");
      go("tests");
    },
    "maintain-workspace": () => {
      if (!root) {
        return;
      }
      openStorage("backup");
    },
    "check-staged-upgrade": () => {
      if (!root) {
        return;
      }
      openStorage("upgrade");
    },
    "manage-scenarios": () => {
      setTestsView("scenarios");
      go("tests");
    },
    "manage-assertions": () => {
      setTestsView("assertions");
      go("tests");
    },
    "inspect-raw-file": () => {
      setToolsView("inspect");
      go("tools");
      setRawRequest((count) => count + 1);
    },
    "performance-corpus": () => {
      setToolsView("benchmarks");
      go("tools");
      setCorpusRequest((count) => count + 1);
    },
    "cancel-operation": () => cancel(),
    "next-region": () => step(1),
    "previous-region": () => step(-1),
    "go-to-commands": () => focusRegion("commands"),
    "go-to-navigation": () => focusRegion("navigation"),
    "go-to-evidence": () => focusRegion("evidence"),
    "go-to-inspector": () => focusRegion("inspector"),
    "go-to-privacy": () => focusRegion("privacy"),
    "larger-text": () =>
      setScale((current) => Math.min((described?.text_scales.length ?? 1) - 1, current + 1)),
    "smaller-text": () => setScale((current) => Math.max(0, current - 1)),
    "switch-theme": () => {
      const themes = described?.themes ?? ["system"];
      const next = themes[(themes.indexOf(theme) + 1) % themes.length];
      setTheme(next ?? "system");
    },
  };

  // The keyboard listener is registered once and reads the current actions, so
  // a shortcut never runs a stale one and the window never re-binds its keys.
  const latest = useRef(actions);
  useEffect(() => {
    latest.current = actions;
  });

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const chosen = ((): CommandId | null => {
        if (event.key === "F6") {
          return event.shiftKey ? "previous-region" : "next-region";
        }
        // An open native dialog owns Escape. Do not prevent its dismissal
        // or cancel unrelated work behind it.
        if (event.key === "Escape") {
          return document.querySelector("dialog[open]") ? null : "cancel-operation";
        }
        if (!(event.ctrlKey || event.metaKey)) {
          return null;
        }
        switch (event.key.toLowerCase()) {
          case "k":
            return "command-palette";
          case "f":
            return "search-workspace";
          case "o":
            return "open-workspace";
          case "=":
          case "+":
            return "larger-text";
          case "-":
            return "smaller-text";
          case "s":
            return event.shiftKey ? "manage-scenarios" : null;
          case "a":
            return event.shiftKey ? "manage-assertions" : null;
          default:
            return null;
        }
      })();
      if (chosen) {
        event.preventDefault();
        latest.current[chosen]();
      }
    };
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, [paletteOpen]);

  const listed = (described?.commands ?? []).filter((command) => {
    const wanted = paletteQuery.trim().toLowerCase();
    return (
      wanted === "" ||
      command.title.toLowerCase().includes(wanted) ||
      (command.keys ?? "").toLowerCase().includes(wanted)
    );
  });

  const artifacts = opened?.artifacts ?? [];
  const named = (kind: string) => artifacts.filter((artifact) => artifact.kind === kind).map((artifact) => artifact.name);
  const caseBundles = artifacts.filter((artifact) => artifact.kind === "case");
  const otherEntries = artifacts.filter((artifact) => artifact.kind !== "case");
  const projectName = overview?.title || (root ? folderName(root) : "");
  const caseTitle = verified ? overview?.cases.find((entry) => entry.name === verified.name)?.title || verified.name : "";
  const subpage = importing && root ? "import" : capturing && root ? "capture" : observing && root ? "observe" : null;
  const inspecting = selectedOccurrence !== null || inspectionResult !== null || running === "inspect";
  const detailsShown = destination === "cases" && verified !== null && subpage === null && inspecting;
  const inspected =
    selectedOccurrence && inspectionResult?.inspection
      ? { occurrence: selectedOccurrence, path: inspectionResult.inspection.selected.path }
      : null;
  const closeDetails = () => {
    setSelectedOccurrence(null);
    setInspectionResult(null);
    focusRegion("evidence");
  };
  const leaveSubpage = () => {
    if (importing) {
      setImporting(false);
      void leaveImport();
    }
    setCapturing(false);
    setObserving(false);
    setCaptureBinding(null);
  };
  const noProject = (what: string) => (
    <EmptyState
      title={`Open a project to see its ${what}`}
      action={
        <>
          <button type="button" className="primary" disabled={busy} onClick={() => go("home")}>
            Go to projects
          </button>
        </>
      }
    />
  );
  const interruptible = running === "workspace" || running === "diagnosis-groups";

  // Every region the facade declares has an element here, for the same reason
  // every command has an action: a region the window forgot is a type error.
  // ReactElement rather than ReactNode, because ReactNode admits null and would
  // accept a region entered as nothing. What a region then draws is beyond it.
  const content: Record<RegionId, ReactElement> = {
    commands: (
      <>
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            R
          </span>
          <span className="brand-name">Readmit</span>
          <button type="button" className="command-button" title="Search commands (⌘K)" onClick={actions["command-palette"]}>
            <span className="visually-hidden">Search commands</span>
            <kbd aria-hidden="true">⌘K</kbd>
          </button>
        </div>
        {root ? (
          <button type="button" className="nav-item search-button" onClick={actions["search-workspace"]}>
            <svg className="nav-icon" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round">
              <circle cx="7" cy="7" r="4.5" />
              <path d="M10.5 10.5 14 14" />
            </svg>
            <span className="nav-label">Search</span>
            <kbd aria-hidden="true">⌘F</kbd>
          </button>
        ) : null}
      </>
    ),
    navigation: (
      <>
        <ul className="nav-list">
          <NavItem id="home" label="Projects" current={destination === "home"} onSelect={go} />
        </ul>
        {root ? (
          <>
            <p className="nav-section-label">Project</p>
            <div className="project-switcher">
              <span className="project-name" title={projectName}>
                {projectName}
              </span>
              <span className="project-path" title={root}>
                {root}
              </span>
            </div>
            <ul className="nav-list">
              {PROJECT_DESTINATIONS.map((item) => (
                <NavItem
                  key={item.id}
                  id={item.id}
                  label={item.label}
                  current={destination === item.id}
                  onSelect={go}
                />
              ))}
            </ul>
          </>
        ) : null}
        <ul className="nav-list nav-footer">
          {GLOBAL_DESTINATIONS.map((item) => (
            <NavItem key={item.id} id={item.id} label={item.label} current={destination === item.id} onSelect={go} />
          ))}
        </ul>
      </>
    ),
    evidence: (
      <>
        <Page
          id="home"
          shown={destination === "home"}
          title="Projects"
          actions={
            <>
              <button type="button" disabled={busy} onClick={actions["open-workspace"]}>
                Open…
              </button>
              <button type="button" className="primary" disabled={busy} onClick={() => setCreatingProject(true)}>
                New project
              </button>
            </>
          }
        >
          <Recovery restored={restored} onChanged={() => void restore()} onReopen={() => void reopen()} />
          <RetainedDrafts drafts={drafts} onDiscardDraft={dropDraft} />
          {openNotice ? (
            <div className="notice danger">
              <Report indicators={indicators} progress={null} result={openNotice} />
              <button type="button" className="action-retry-folder" disabled={busy} onClick={actions["open-workspace"]}>
                Choose another folder…
              </button>
            </div>
          ) : null}
          <RecentWorkspaces
            recent={recent}
            busy={busy}
            indicators={indicators}
            onReopen={(folder) => void openFolder(() => openWorkspace(folder))}
            onForget={forget}
          />
          <DemoCallout busy={busy} onTryDemo={actions["create-sample-workspace"]} />
        </Page>

        <Page
          id="cases"
          shown={destination === "cases"}
          eyebrow={
            subpage !== null || verified ? (
              <nav aria-label="Where you are" className="page-eyebrow">
                <button type="button" onClick={subpage !== null ? leaveSubpage : backToProject}>
                  {projectName || "Cases"}
                </button>
                <span aria-hidden="true">›</span>
                {verified && caseFlow !== null && subpage === null ? (
                  <>
                    <button type="button" onClick={() => setCaseFlow(null)}>
                      {caseTitle}
                    </button>
                    <span aria-hidden="true">›</span>
                  </>
                ) : null}
              </nav>
            ) : null
          }
          title={
            subpage === "import"
              ? "Import evidence"
              : subpage === "capture"
                ? "Capture"
                : subpage === "observe"
                  ? "Observations"
                  : verified
                    ? caseFlow !== null
                      ? CASE_FLOW_TITLES[caseFlow]
                      : caseTitle
                    : projectName || "Cases"
          }
          subtitle={
            verified && subpage === null && caseFlow === null ? (
              <>
                {verified.messages} {verified.messages === 1 ? "message" : "messages"} · {verified.acknowledgements}{" "}
                {verified.acknowledgements === 1 ? "ACK" : "ACKs"} · {verified.sources} {verified.sources === 1 ? "source" : "sources"} ·{" "}
                {humanize(verified.provenance)}
              </>
            ) : null
          }
          actions={
            root && subpage === null && !verified ? (
              <>
                <button type="button" disabled={busy} onClick={() => setObserving(true)}>
                  Observations
                </button>
                <button type="button" disabled={busy} onClick={() => setCapturing(true)}>
                  Capture
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => setImporting(true)}>
                  Import
                </button>
              </>
            ) : verified && subpage === null && caseFlow === null ? (
              <>
                <button type="button" disabled={busy} onClick={() => setCaseFlow("replay")}>
                  Replay…
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => setCaseFlow("test")}>
                  Create test
                </button>
                <MoreMenu
                  label="More case actions"
                  items={[
                    { label: "Compare with another case", onSelect: () => setCaseFlow("compare") },
                    { label: "Build a reproducer", onSelect: () => setCaseFlow("reproduce") },
                    { label: "Reduce", onSelect: () => setCaseFlow("reduce") },
                    { label: "File details", onSelect: () => setFileDetails(true) },
                    { label: "Close case", onSelect: backToProject },
                  ]}
                />
              </>
            ) : null
          }
        >
          {!root ? noProject("cases") : null}
          {observing && root ? (
            <ObservationPanel
              workspace={root}
              busy={busy}
              indicators={indicators}
              drafts={drafts ?? []}
              captureBinding={captureBinding}
              onBindToTest={(observationFile) => {
                setObserving(false);
                setCaptureBinding(null);
                void author((draft, open) =>
                  authorTest({
                    workspace: root ?? "",
                    case: open.case,
                    identity: open.identity,
                    draft,
                    answer: { stage: "observation", observation: observationFile },
                  }),
                );
              }}
              onClose={() => {
                setObserving(false);
                setCaptureBinding(null);
              }}
            />
          ) : capturing && root ? (
            <CapturePanel
              workspace={root}
              project={investigation?.overview?.root ?? null}
              busy={busy}
              indicators={indicators}
              onOpenCase={(name) => {
                setCapturing(false);
                void verifyCase(root, name);
              }}
              onSetupIndex={(name) => {
                setCapturing(false);
                void verifyCase(root, name);
              }}
              onBindObservation={(binding) => {
                setCapturing(false);
                setCaptureBinding(binding);
                setObserving(true);
              }}
              onClose={() => setCapturing(false)}
            />
          ) : importing && root ? (
            <ImportPanel
              workspace={root}
              project={investigation?.overview?.root ?? null}
              drafts={drafts}
              busy={busy}
              indicators={indicators}
              onOpenCase={(name) => {
                setImporting(false);
                void leaveImport().then(() => verifyCase(root, name));
              }}
              onSetupIndex={(name) => {
                setImporting(false);
                void leaveImport().then(() => verifyCase(root, name));
              }}
              onClose={() => {
                setImporting(false);
                void leaveImport();
              }}
            />
          ) : null}

          {root ? (
            <div className="cases-list" hidden={subpage !== null || verified !== null}>
              {guideResult?.guide ? (
                <GuidedSample
                  result={guideResult}
                  practice={practiceResult}
                  capture={sampleCapture}
                  busy={busy}
                  progress={
                    running === "practice"
                      ? "Running the saved test against the practice receiver."
                      : running === "sample"
                        ? "Importing the frozen receiver fixtures."
                        : null
                  }
                  indicators={indicators}
                  onCreateSample={actions["create-sample-workspace"]}
                  onOpenCase={(name: string) => {
                    if (root) void verifyCase(root, name);
                  }}
                  onRun={(trial, output) => void practise(trial, output)}
                  onCancel={cancelRunning}
                  onCapture={(output) => void importSample(output)}
                />
              ) : null}
              {workspace && workspace.state !== "completed" ? (
                <Report indicators={indicators} progress={null} result={workspace} />
              ) : null}
              {openNotice ? (
                <div className="notice danger">
                  <Report indicators={indicators} progress={null} result={openNotice} />
                  <button type="button" className="action-retry-folder" disabled={busy} onClick={actions["open-workspace"]}>
                    Choose another folder…
                  </button>
                </div>
              ) : null}
              {caseNotice ? (
                <div className="notice danger">
                  <Report indicators={indicators} progress={null} result={caseNotice} />
                </div>
              ) : null}
              {/* A folder holding a project lists its cases in the project's own
                  table below, registered or not, so the page has one list. */}
              {overview ? null : (
              <>
              <div className="section-title">
                <h2>Cases</h2>
                <span className="count">{caseBundles.length}</span>
              </div>
              {caseBundles.length === 0 ? (
                <EmptyState
                  title="No cases yet"
                  action={
                    <button type="button" className="primary" disabled={busy} onClick={() => setImporting(true)}>
                      Import evidence
                    </button>
                  }
                >
                  Import exported messages or capture them from a feed to start an investigation.
                </EmptyState>
              ) : (
                <ul className="item-list artifacts" aria-label="Cases in this folder">
                  {caseBundles.map((artifact) => (
                    <li key={artifact.name} aria-current={selected === artifact.name ? "true" : undefined}>
                      <span className="item-main">
                        <span className="item-title">{artifact.name}</span>
                        <span className="item-sub">{humanize(artifact.provenance ?? "")}</span>
                      </span>
                      <span className="item-actions">
                        <button
                          type="button"
                          disabled={busy}
                          aria-label={`Open case ${artifact.name}`}
                          onClick={() => void verifyCase(opened?.root ?? root, artifact.name)}
                        >
                          Open
                        </button>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              </>
              )}
              <ProjectPanel
                root={root}
                result={investigation}
                entries={artifacts}
                busy={busy}
                progress={running === "project" ? "Reading the project." : null}
                indicators={indicators}
                selectedCase={verified?.name ?? null}
                editable={editable}
                onUpdateSettings={editSettings}
                onReadEditable={() => void readEditable()}
                onRegister={(name, registration) => void register(name, registration)}
                onUpdateCase={(name, change) => void updateCase(name, change)}
                onOpenCase={(name: string) => {
                  if (root) void verifyCase(root, name);
                }}
              />
              {otherEntries.length > 0 ? (
                <details className="other-entries">
                  <summary>Other files in this folder ({otherEntries.length})</summary>
                  <ul className="artifacts">
                    {otherEntries.map((artifact) => (
                      <li key={artifact.name} aria-current={selected === artifact.name ? "true" : undefined}>
                        <span className="name">{artifact.name}</span>
                        <Badge indicator={indicators.get(artifact.kind)} fallback={artifact.kind} />
                        {artifact.kind === "project" ? (
                          <button type="button" disabled={busy} onClick={() => void readProject(root)}>
                            Open project
                          </button>
                        ) : null}
                        {artifact.kind === "target" || artifact.kind === "secret" || artifact.kind === "policy" || artifact.kind === "reset" ? (
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() => {
                              setSelected(artifact.name);
                              setEnvironmentArtifact({
                                name: artifact.name,
                                kind: artifact.kind === "secret" ? "secrets" : artifact.kind as "target" | "policy" | "reset",
                              });
                              go("environments");
                            }}
                          >
                            {artifact.kind === "target"
                              ? "Configure target"
                              : artifact.kind === "secret"
                                ? "Manage secrets"
                                : artifact.kind === "policy"
                                  ? "Review policy"
                                  : "View reset plan"}
                          </button>
                        ) : null}
                        {artifact.reason ? (
                          <span className={artifact.kind === "unsupported" ? "unsupported" : "reason"}>{artifact.reason}</span>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                </details>
              ) : null}
            </div>
          ) : null}

          {verified && root ? (
            <div className="case-view" hidden={subpage !== null}>
              {caseNotice ? (
                <div className="notice danger">
                  <Report indicators={indicators} progress={null} result={caseNotice} />
                  <button type="button" className="action-back-to-listing" onClick={backToProject}>
                    Back to cases
                  </button>
                </div>
              ) : null}
              <div hidden={caseFlow !== null}>
              <TaskTabs label="Case views" id="case-views" tabs={CASE_VIEWS} selected={caseView} onSelect={setCaseView} keepMounted>
                <TaskPanel tabs="case-views" tab="messages" className="task-panel view-panel" shown={caseView === "messages"}>
                  <Report indicators={indicators} progress={running === "case" ? "Verifying the case." : null} result={null} />
                  <MessageGrid
                    indicators={indicators}
                    progress={running === "grid" ? "Reading this window of the case." : null}
                    result={gridResult}
                    filters={savedFilters}
                    entries={named("index")}
                    busy={busy}
                    onOpen={(indexName, offset) => {
                      if (root) void showGrid(root, verified.name, indexName, offset);
                    }}
                    onSelect={(name) => void changeFilters(() => selectFilter(name))}
                    onSave={(filter) => void changeFilters(() => saveFilter(filter))}
                    selectedOccurrence={selectedOccurrence}
                    onInspect={(occurrence) => void inspect(occurrence, "", 0, -1)}
                    caseEvidence={evidence?.case}
                    indexDetails={indexResult?.index}
                    onBuildIndex={handleBuildIndex}
                  />
                </TaskPanel>
                <TaskPanel tabs="case-views" tab="timeline" className="task-panel view-panel" shown={caseView === "timeline"}>
                  <Sequence
                    workspace={root}
                    caseIdentity={verified.identity}
                    onSaved={() => void refreshListing()}
                    onReview={async (request, write) => {
                      let result: CorrelationReviewResult = { state: "failed", reason: "The review did not run." };
                      await run("correlation-review", async () => {
                        result = await (write ? decideCorrelation(request) : openCorrelationReview(request));
                      });
                      // A recorded decision is a new directory of the open folder, so
                      // the listing is read again and the retained reviews offer it.
                      if (write && result.state === "completed") await refreshListing();
                      return result;
                    }}
                    rulesEntries={named("rules")}
                    analyses={named("analysis")}
                    reviews={named("correlation-review")}
                    result={sequenceResult}
                    busy={busy}
                    active={destination === "cases" && caseView === "timeline" && caseFlow === null}
                    progress={running === "sequence" ? "Loading the timeline." : null}
                    indicators={indicators}
                    onOpen={(rules, offset, analysis) => void layOutSequence(rules, offset, analysis)}
                    onSelect={(occurrence) => void inspect(occurrence, "", 0, -1)}
                  />
                </TaskPanel>
                <TaskPanel tabs="case-views" tab="findings" className="task-panel view-panel" shown={caseView === "findings"}>
                  <Diagnosis
                    workspace={root}
                    caseName={verified.name}
                    identity={verified.identity}
                    configEntries={named("diagnose-config")}
                    reportEntries={named("diagnosis")}
                    groupsReportEntries={named("diagnosis-groups")}
                    caseEntries={named("case")}
                    decisionsEntries={named("finding-decisions")}
                    result={diagnosisResult}
                    groupsResult={diagnosisGroupsResult}
                    reviewResult={findingReviewResult}
                    busy={busy}
                    progress={
                      running === "diagnosis"
                        ? "Running this diagnosis."
                        : running === "diagnosis-report"
                          ? "Opening this report."
                          : running === "finding-review"
                            ? "Reviewing these findings."
                            : null
                    }
                    groupsProgress={
                      running === "diagnosis-groups"
                        ? "Grouping findings across these cases."
                        : running === "diagnosis-groups-report"
                          ? "Opening this grouping report."
                          : null
                    }
                    indicators={indicators}
                    onRun={(request) => void diagnose(request)}
                    onOpen={(entry, offset) => void openReport(entry, offset)}
                    onGroup={(request) => void groupCases(request)}
                    onOpenGroups={(entry, offset) => void openGroupsReport(entry, offset)}
                    onReview={(request, write) => void reviewDiagnosisFindings(request, write)}
                    onSelect={(occurrence) => void inspect(occurrence, "", 0, -1)}
                    onPromote={(status, reviewEntry, reportSHA256) =>
                      void promoteFinding(status, reviewEntry, reportSHA256)
                    }
                    onManageProfiles={actions["manage-profiles"]}
                    onSaved={() => void refreshListing()}
                  />
                </TaskPanel>
              </TaskTabs>
              </div>
                <div className="view-panel case-flow" hidden={caseFlow !== "compare"}>
                  <Comparison
                    entries={named("case")}
                    result={comparisonResult}
                    busy={busy}
                    progress={
                      running === "comparison"
                        ? "Comparing these collections."
                        : running === "normalize"
                          ? "Reading this comparison under the declared policy."
                          : null
                    }
                    indicators={indicators}
                    onCompare={(right, keys, fields, offset) =>
                      void compareCollections(right, keys, fields, offset)
                    }
                    workspace={root}
                    policyEntries={named("normalization-policy")}
                    normalizeResult={normalizeResult}
                    onNormalize={(right, policy, keys, fields, offset) =>
                      void normalizeCollections(right, policy, keys, fields, offset)
                    }
                    onSaved={() => void refreshListing()}
                  />
                  <RevisionComparison
                    entries={artifacts.map((artifact) => artifact.name)}
                    seed={comparisonSeed}
                    result={revisionResult}
                    busy={busy}
                    progress={running === "revisions" ? "Comparing these revisions." : null}
                    indicators={indicators}
                    onCompare={(left, right, leftResult, rightResult) =>
                      void compareRevisions(left, right, leftResult, rightResult)
                    }
                  />
                </div>
                <div className="view-panel case-flow" hidden={caseFlow !== "reproduce"}>
                  {gridResult?.grid ? (
                    <Reproducer
                      rows={gridResult.grid.rows}
                      result={reproducerResult}
                      restoredDraft={Boolean(reproducerResult?.reproducer && !reproducerResult.reproducer.resolution)}
                      onDiscardDraft={discardReproducerDraft}
                      inspected={inspected}
                      busy={busy}
                      progress={running === "reproducer" ? "Resolving this reproducer against the case." : null}
                      indicators={indicators}
                      onStep={(step: ReproducerStep) =>
                        void reproduce((plan, open) =>
                          editReproducer({
                            workspace: root ?? "",
                            case: open.case,
                            identity: open.identity,
                            plan,
                            step,
                          }),
                        )
                      }
                      onUndo={() =>
                        void reproduce((plan, open) =>
                          undoReproducer({
                            workspace: root ?? "",
                            case: open.case,
                            identity: open.identity,
                            plan,
                          }),
                        )
                      }
                      parentCase={gridResult.grid.case}
                      onBuild={(output: string) =>
                        void reproduce((plan, open) =>
                          buildReproducer({
                            workspace: root ?? "",
                            case: open.case,
                            identity: open.identity,
                            plan,
                            output,
                          }),
                        )
                      }
                      onRegister={registerBuiltRevision}
                      onOpenRevision={(name) => {
                        if (root) void verifyCase(root, name);
                      }}
                      onCompareRevision={(built) => {
                        compareBuild(built);
                        setCaseFlow("compare");
                      }}
                      onCreateTest={(name) => {
                        // Open the revision as its own case. An existing test draft stays
                        // bound to the original case identity and is not retargeted.
                        if (root) void verifyCase(root, name);
                      }}
                    />
                  ) : (
                    <EmptyState title="Messages are not loaded yet">
                      A reproducer is built from the messages of this case. Open them in Messages first.
                    </EmptyState>
                  )}
                </div>
                <div className="view-panel case-flow" hidden={caseFlow !== "test"}>
                  {gridResult?.grid ? (
                    <TestAuthoring
                      rows={gridResult.grid.rows}
                      result={testResult}
                      restoredDraft={Boolean(testResult?.test && !testResult.test.resolution)}
                      provenance={promotionProvenance}
                      onDiscardDraft={discardTestDraft}
                      inspected={inspected}
                      busy={busy}
                      progress={running === "authoring" ? "Resolving this test against the case." : null}
                      indicators={indicators}
                      onAnswer={(answer: TestAnswer) =>
                        void author((draft, open) =>
                          authorTest({
                            workspace: root ?? "",
                            case: open.case,
                            identity: open.identity,
                            draft,
                            answer,
                          }),
                        )
                      }
                      onSave={(output: string) =>
                        void author((draft, open) =>
                          saveTest({
                            workspace: root ?? "",
                            case: open.case,
                            identity: open.identity,
                            draft,
                            output,
                          }),
                        )
                      }
                      onSuggest={(suggest: TestSuggestionRequest) =>
                        void author((draft, open) =>
                          suggestExpectations({
                            workspace: root ?? "",
                            case: open.case,
                            identity: open.identity,
                            draft,
                            suggest,
                          }),
                        )
                      }
                      onApprove={(suggest: TestSuggestionRequest, review: TestReview) =>
                        void author((draft, open) =>
                          approveExpectations({
                            workspace: root ?? "",
                            case: open.case,
                            identity: open.identity,
                            draft,
                            suggest,
                            review,
                          }),
                        )
                      }
                    />
                  ) : (
                    <EmptyState title="Messages are not loaded yet">
                      A test is written over the messages of this case. Open them in Messages first.
                    </EmptyState>
                  )}
                </div>
                <div className="view-panel case-flow" hidden={caseFlow !== "reduce"}>
                  <Reduction
                    ruleEntries={named("rules")}
                    specEntries={named("spec")}
                    targetEntries={named("target")}
                    resetEntries={named("reset")}
                    policyEntries={named("policy")}
                    result={reductionResult}
                    busy={busy}
                    progress={
                      running === "reduction-preview"
                        ? "Previewing how this reduction would take the sequence apart. Nothing is reset or sent."
                        : running === "reduction"
                          ? "Running this reduction: every trial resets the environment, then sends."
                          : null
                    }
                    reducing={running === "reduction"}
                    indicators={indicators}
                    caseOpen={verified !== null}
                    onPreview={(config) => void runReductionPreview(config)}
                    onStart={(config) => void runReduction(config)}
                    onCancel={cancelRunning}
                  />
                </div>
                <div className="view-panel case-flow" hidden={caseFlow !== "replay"}>
                  <ReplayPanel
                    key={"replay-" + root + verified.identity}
                    workspace={root}
                    caseName={verified.name}
                    identity={verified.identity}
                    rows={gridResult?.grid?.case === verified.name && gridResult.grid.identity === verified.identity ? gridResult.grid.rows : []}
                    entries={artifacts}
                    busy={busy}
                    onSent={() => void refreshListing()}
                  />
                </div>

            </div>
          ) : null}
        </Page>

        <Page id="tests" shown={destination === "tests"} title="Tests">
          {root ? (
            <TaskTabs label="Test views" id="tests-views" tabs={TESTS_VIEWS} selected={testsView} onSelect={setTestsView} keepMounted>
              <TaskPanel tabs="tests-views" tab="suites" className="task-panel view-panel" shown={testsView === "suites"}>
                <SuitePanel
                  key={"suite-" + root}
                  workspace={root}
                  busy={busy}
                  entries={artifacts}
                  drafts={drafts}
                  onExecute={handoff}
                  onSaved={() => void refreshListing()}
                />
              </TaskPanel>
              <TaskPanel tabs="tests-views" tab="tests" className="task-panel view-panel" shown={testsView === "tests"}>
                <CanonicalTestEditor key={"editor-" + root} workspace={root} drafts={drafts} busy={busy} />
              </TaskPanel>
              <TaskPanel tabs="tests-views" tab="assertions" className="task-panel view-panel" shown={testsView === "assertions"}>
                <AssertionSetAuthoring key={"assertions-" + root} workspace={root} drafts={drafts} busy={busy} inspected={inspected} />
              </TaskPanel>
              <TaskPanel tabs="tests-views" tab="profiles" className="task-panel view-panel" shown={testsView === "profiles"}>
                <ProfileEditor key={`profile-${root}`} workspace={root} drafts={drafts} busy={busy} />
              </TaskPanel>
              <TaskPanel tabs="tests-views" tab="scenarios" className="task-panel view-panel" shown={testsView === "scenarios"}>
                <ScenarioPanel
                  key={`scenario-${root}`}
                  workspace={root}
                  drafts={drafts}
                  busy={busy}
                  indicators={indicators}
                  onOpenCase={(name) => {
                    if (root) void verifyCase(root, name);
                  }}
                  onStartTestDraft={(name) => {
                    if (root) void verifyCase(root, name);
                  }}
                />
              </TaskPanel>
              <TaskPanel tabs="tests-views" tab="baselines" className="task-panel view-panel" shown={testsView === "baselines"}>
                <Baseline key={root} workspace={root} busy={busy} onSaved={() => void refreshListing()} />
              </TaskPanel>
            </TaskTabs>
          ) : (
            noProject("tests")
          )}
        </Page>

        <Page id="runs" shown={destination === "runs"} title="Runs">
          {root ? (
            <TaskTabs label="Run views" id="runs-views" tabs={RUNS_VIEWS} selected={runsView} onSelect={setRunsView} keepMounted>
              <TaskPanel tabs="runs-views" tab="run" className="task-panel view-panel" shown={runsView === "run"}>
                {suiteHandoff && suiteHandoff.workspace === root ? <SuiteHandoffNotice handoff={suiteHandoff.handoff} /> : null}
                <RunPanel
                  workspace={root}
                  entries={artifacts}
                  onWatch={watch}
                  onRefresh={() => void refreshListing()}
                  onOpenCase={(name: string) => {
                    if (root) void verifyCase(root, name);
                  }}
                  onConfigureEnvironment={() => go("environments")}
                  onOpenLicense={() => openSettings("license")}
                  {...(runSpecPath ? { initialSpec: runSpecPath } : {})}
                  {...(runEnvironment ? { initialEnvironment: runEnvironment } : {})}
                />
              </TaskPanel>
              <TaskPanel tabs="runs-views" tab="details" className="task-panel view-panel" shown={runsView === "details"}>
                <RunExplanation key={"explain-" + root} workspace={root} entries={artifacts} busy={busy} />
              </TaskPanel>
              <TaskPanel tabs="runs-views" tab="compare" className="task-panel view-panel" shown={runsView === "compare"}>
                <RunComparison key={"runs-" + root} workspace={root} busy={busy} entries={artifacts} />
              </TaskPanel>
            </TaskTabs>
          ) : (
            noProject("runs")
          )}
        </Page>

        <Page id="environments" shown={destination === "environments"} title="Environments">
          {opened ? (
            <EnvironmentPanel
              workspace={root ?? ""}
              targetFile={environmentArtifact?.kind === "target" ? environmentArtifact.name : "targets/default.json"}
              secretsFile={environmentArtifact?.kind === "secrets" ? environmentArtifact.name : "secrets.json"}
              policyFile={environmentArtifact?.kind === "policy" ? environmentArtifact.name : "send-policy.json"}
              planFile={environmentArtifact?.kind === "reset" ? environmentArtifact.name : "reset-plan.json"}
              initialTab={environmentArtifact?.kind ?? "target"}
              drafts={drafts}
              onPlanSaved={() => void refreshListing()}
            />
          ) : (
            noProject("environments")
          )}
        </Page>

        <Page id="reports" shown={destination === "reports"} title="Reports">
          {root ? (
            <TaskTabs label="Report views" id="reports-views" tabs={REPORTS_VIEWS} selected={reportsView} onSelect={setReportsView} keepMounted>
              <TaskPanel tabs="reports-views" tab="packets" className="task-panel view-panel" shown={reportsView === "packets"}>
                <PacketPanel workspace={root} entries={artifacts} onRefresh={() => void refreshListing()} />
              </TaskPanel>
              <TaskPanel tabs="reports-views" tab="disclosure" className="task-panel view-panel" shown={reportsView === "disclosure"}>
                <PrivacyPanel workspace={root} entries={artifacts} drafts={drafts} onRefresh={() => void refreshListing()} />
              </TaskPanel>
              <TaskPanel tabs="reports-views" tab="export" className="task-panel view-panel" shown={reportsView === "export"}>
                <Review
                  ruleEntries={named("rules")}
                  planEntries={named("plan")}
                  packEntries={named("pack")}
                  reviewEntries={named("review")}
                  transformResult={transformResult}
                  planResult={planResult}
                  reviewResult={reviewResult}
                  caseOpen={verified !== null}
                  caseIdentity={verified?.identity ?? ""}
                  busy={busy}
                  transformProgress={running === "transformation" ? "Previewing this transformation." : null}
                  planProgress={
                    running === "transform-plan"
                      ? "Saving this transformation plan."
                      : running === "transform-open"
                        ? "Reading this transformation plan."
                        : null
                  }
                  reviewProgress={running === "review" ? "Reading this export review." : null}
                  indicators={indicators}
                  onPreview={(rules, plan, profile) => void previewPlan(rules, plan, profile)}
                  onSavePlan={savePlan}
                  onOpenPlan={openPlan}
                  onReview={(review, approve, offset) => void readReview(review, approve, offset)}
                />
              </TaskPanel>
              <TaskPanel tabs="reports-views" tab="notes" className="task-panel view-panel" shown={reportsView === "notes"}>
                <NoteDraft project={workspaceRoot} drafts={drafts} restored={restored} onChanged={() => void restore()} />
              </TaskPanel>
            </TaskTabs>
          ) : (
            noProject("reports")
          )}
        </Page>

        <Page
          id="tools"
          shown={destination === "tools"}
          title="Tools"
          actions={
            <button type="button" disabled={busy} onClick={actions["open-workspace"]}>
              Open folder…
            </button>
          }
        >
          <TaskTabs
            label="Tools"
            id="tools-views"
            tabs={TOOLS_VIEWS}
            selected={toolsView}
            onSelect={(view) => {
              setToolsView(view);
              if (view === "inspect") setRawRequest((count) => count + 1);
              else setCorpusRequest((count) => count + 1);
            }}
            keepMounted
          >
            <TaskPanel tabs="tools-views" tab="inspect" className="task-panel view-panel" shown={toolsView === "inspect"}>
              <RawInspection busy={busy} indicators={indicators} request={rawRequest} />
            </TaskPanel>
            <TaskPanel tabs="tools-views" tab="benchmarks" className="task-panel view-panel" shown={toolsView === "benchmarks"}>
              <PerformanceCorpus busy={busy} indicators={indicators} request={corpusRequest} />
            </TaskPanel>
          </TaskTabs>
        </Page>

        <Page id="settings" shown={destination === "settings"} title="Settings">
          <TaskTabs label="Settings" id="settings-views" tabs={SETTINGS_VIEWS} selected={settingsView} onSelect={setSettingsView} keepMounted>
            <TaskPanel tabs="settings-views" tab="general" className="task-panel view-panel" shown={settingsView === "general"}>
              <section className="card" aria-labelledby="appearance-title">
                <h2 id="appearance-title">Appearance</h2>
                <div className="setting-rows">
                  <div className="setting-row">
                    <label htmlFor="theme">Theme</label>
                    <select id="theme" value={theme} onChange={(event) => setTheme(event.target.value as Theme)}>
                      {(described?.themes ?? []).map((choice) => (
                        <option key={choice} value={choice}>
                          {choice === "system" ? "Match system" : choice === "light" ? "Light" : choice === "dark" ? "Dark" : humanize(choice)}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="setting-row">
                    <span id="text-size-label">Text size</span>
                    <div className="text-size" role="group" aria-labelledby="text-size-label">
                      <button type="button" onClick={actions["smaller-text"]} disabled={scale === 0}>
                        Smaller
                      </button>
                      <span className="scale">{described?.text_scales[scale] ?? 100}%</span>
                      <button
                        type="button"
                        onClick={actions["larger-text"]}
                        disabled={scale >= (described?.text_scales.length ?? 1) - 1}
                      >
                        Larger
                      </button>
                    </div>
                  </div>
                </div>
              </section>
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="license" className="task-panel view-panel" shown={settingsView === "license"}>
              <OperationAccess />
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="team" className="task-panel view-panel" shown={settingsView === "team"}>
              <HubPanel workspace={root} entries={artifacts} />
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="runners" className="task-panel view-panel" shown={settingsView === "runners"}>
              <RunnerPanel />
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="security" className="task-panel view-panel" shown={settingsView === "security"}>
              {described ? (
                <PrivacyDisclosure
                  operations={described.privacy.operations}
                  workspaceOpen={root !== null}
                  onOpenRunPanel={() => {
                    setRunsView("run");
                    go("runs");
                  }}
                  onStartCapture={() => {
                    setCapturing(true);
                    go("cases");
                  }}
                  onStartObservation={() => {
                    setCaptureBinding(null);
                    setObserving(true);
                    go("cases");
                  }}
                />
              ) : null}
              {root ? (
                <ProtectionPanel workspace={root} entries={artifacts} onRefresh={() => void refreshListing()} />
              ) : null}
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="storage" className="task-panel view-panel" shown={settingsView === "storage"}>
              {root ? (
                <MaintenancePanel
                  key={`maintenance-${root}-${maintenanceRequest}`}
                  workspace={root}
                  project={investigation?.overview?.root ?? null}
                  busy={busy}
                  indicators={indicators}
                  initialTab={maintenanceTab}
                  onReopen={(path) => {
                    void openFolder(() => openWorkspace(path)).then(() => {
                      void openProjectOverview(path).then((result) => setInvestigation(result));
                    });
                  }}
                  onProjectChanged={(path) => {
                    // Only the project still open takes the answer: a person who
                    // moved on before it arrived keeps what they moved to.
                    void openProjectOverview(path).then((answer) =>
                      setInvestigation((held) =>
                        held?.overview?.root !== path ? held : answer.overview ? answer : { ...answer, overview: held.overview },
                      ),
                    );
                  }}
                  onClose={() => {
                    go("cases");
                  }}
                />
              ) : (
                noProject("storage and backups")
              )}
            </TaskPanel>
          </TaskTabs>
        </Page>

        <Page id="help" shown={destination === "help"} title="Help">
          <HelpTopics />
          <section className="card" aria-labelledby="privacy-help-title" style={{ marginTop: "1rem" }}>
            <h2 id="privacy-help-title">Privacy</h2>
            <p className="statement">{described?.privacy.statement}</p>
            <ul className="absent plain-list">
              {(described?.privacy.absent ?? []).map((claim) => (
                <li key={claim}>{claim}</li>
              ))}
            </ul>
            <h3>Kept on this machine</h3>
            <ul className="kept plain-list">
              {(described?.privacy.kept ?? []).map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </section>
          {described ? <SupportGuidance support={described.support} /> : null}
        </Page>
      </>
    ),
    inspector: (
      <>
        <div className="details-close">
          <IconButton icon="close" label="Close message details" onClick={closeDetails} />
        </div>
        {verified ? (
          <Inspector
            result={inspectionResult}
            row={gridResult?.grid?.rows.find((row) => row.id === selectedOccurrence) ?? null}
            busy={busy}
            progress={running === "inspect" ? "Verifying and inspecting the selected occurrence." : null}
            indicators={indicators}
            onInspect={(path, nodeOffset, byteOffset) => {
              if (selectedOccurrence) void inspect(selectedOccurrence, path, nodeOffset, byteOffset);
            }}
          />
        ) : null}
      </>
    ),
    privacy: (
      <>
        <div className="status-line" aria-live="polite">
          {busy ? (
            <>
              <span className="spinner" aria-hidden="true" />
              <span>{running ? PROGRESS[running] : "Working…"}</span>
              {interruptible ? (
                <button type="button" className="quiet" onClick={() => cancel()}>
                  Cancel
                </button>
              ) : null}
            </>
          ) : null}
        </div>
        <div className="status-links">
          <button type="button" className="quiet" onClick={() => openSettings("security")}>
            Privacy and connections
          </button>
          <button type="button" className="quiet" onClick={() => openSettings("license")}>
            License
          </button>
        </div>
      </>
    ),
  };

  if (!described) {
    return (
      <main className="starting">
        <h1>Readmit</h1>
        <Status indicator={indicators.get(windowState)} state={windowState} reason={windowReason} />
      </main>
    );
  }

  const panes = {
    "--evidence-fraction": `${split}fr`,
    "--inspector-fraction": `${100 - split}fr`,
  } as CSSProperties;

  // Each region the facade declares, drawn where it sits: the sidebar holds
  // search and navigation, the work area the page and message details, and the
  // status line runs along the bottom. The order they are drawn in is the order
  // the facade declares, which is the order Tab and F6 move through them.
  const region = (id: RegionId, hidden = false) => {
    const declared = described.regions.find((entry) => entry.id === id);
    if (!declared) return null;
    return (
      <section
        className={`region region-${id}`}
        aria-label={declared.label}
        tabIndex={-1}
        hidden={hidden}
        ref={(element) => {
          regionElements.current[id] = element;
        }}
        onFocus={() => setFocused(id)}
      >
        {content[id]}
      </section>
    );
  };

  return (
    <IndicatorsContext.Provider value={indicators}>
      <VocabularyContext.Provider value={described.vocabulary}>
        <div className="app">
          <aside className="sidebar">
            {region("commands")}
            {region("navigation")}
          </aside>
          <div className={`workarea${detailsShown ? " with-details" : ""}`} style={panes}>
            {region("evidence")}
            {detailsShown ? (
              <Separator
                split={split}
                min={MIN_SPLIT}
                max={MAX_SPLIT}
                step={SPLIT_STEP}
                onSplit={setSplit}
                bounds={() => {
                  const left = regionElements.current.evidence?.getBoundingClientRect().left;
                  const right = regionElements.current.inspector?.getBoundingClientRect().right;
                  return left === undefined || right === undefined ? null : { left, right };
                }}
              />
            ) : null}
            {region("inspector", !detailsShown)}
          </div>
          {region("privacy")}
        </div>

        <Modal open={fileDetails && verified !== null} title="File details" onClose={() => setFileDetails(false)}>
          {verified ? (
            <dl className="facts">
              <div className="fact">
                <dt>Case</dt>
                <dd>{verified.name}</dd>
              </div>
              <div className="fact">
                <dt>Origin</dt>
                <dd>{humanize(verified.provenance)}</dd>
              </div>
              <div className="fact">
                <dt>Messages</dt>
                <dd>{verified.messages}</dd>
              </div>
              <div className="fact">
                <dt>ACKs</dt>
                <dd>{verified.acknowledgements}</dd>
              </div>
              <div className="fact">
                <dt>Unparsed</dt>
                <dd>{verified.unparsed}</dd>
              </div>
              <div className="fact">
                <dt>Sources</dt>
                <dd>{verified.sources}</dd>
              </div>
              <div className="fact">
                <dt>Format</dt>
                <dd>{verified.schema}</dd>
              </div>
              <div className="fact">
                <dt>SHA-256</dt>
                <dd className="identity">{verified.identity}</dd>
              </div>
            </dl>
          ) : null}
        </Modal>

        <Modal open={searching} title="Search this project" onClose={() => setSearching(false)}>
          <form
            className="search-form"
            role="search"
            onSubmit={(event) => {
              event.preventDefault();
              if (root && !busy && query.trim() !== "") {
                void searchWorkspace(root, query);
              }
            }}
          >
            <label htmlFor="workspace-search" className="visually-hidden">
              Search this project
            </label>
            <input
              id="workspace-search"
              ref={searchField}
              type="search"
              autoFocus
              placeholder="Cases, notes, message content…"
              value={query}
              disabled={busy}
              onChange={(event) => setQuery(event.target.value)}
            />
          </form>
          <Report indicators={indicators} progress={running === "search" ? "Searching this project." : null} result={found && found.state !== "completed" ? found : null} />
          {found && found.state === "completed" && found.matches.length === 0 ? <p className="hint">No matches.</p> : null}
          {found && found.matches.length > 0 ? (
            <ul className="search-matches" aria-label="Search results">
              {found.matches.map((match) => (
                <li key={`${match.kind}:${match.name}:${match.field}:${match.occurrence ?? ""}`}>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      setSelected(match.name);
                      setSearching(false);
                      openMatch(match);
                    }}
                  >
                    <span className="name">{match.label}</span>
                    <span className="badge">{match.kind === "content" ? "Message" : "Details"}</span>
                    <span className="reason">{humanize(match.field)}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : null}
        </Modal>

        <Modal open={creatingProject} title="New project" onClose={() => setCreatingProject(false)}>
          <NewProjectForm
            busy={busy}
            onCancel={() => setCreatingProject(false)}
            onCreate={async (name, title, owner, versions) => {
              const answer = await startProject(name, title, owner, versions);
              // A new project answers with its overview, whose state is empty
              // until it registers a case: the overview is what says it exists.
              if (answer.overview) {
                setCreatingProject(false);
                return { state: "completed" as const };
              }
              return { state: answer.state, reason: answer.reason };
            }}
          />
        </Modal>

        <Palette
          open={paletteOpen}
          commands={listed}
          query={paletteQuery}
          onQuery={setPaletteQuery}
          onClose={() => setPaletteOpen(false)}
          onRun={(command) => latest.current[command]()}
        />
      </VocabularyContext.Provider>
    </IndicatorsContext.Provider>
  );
}

/** What Go to runs handed over from the suite panel, beside the run view it
 * seeded. The run view selects the suite entry and environment and preflights
 * them itself; its preflight takes no release pins and does not read the
 * prepared folder, so the notice names them only as what the person saw and
 * says so, rather than implying they apply to the run. */
function SuiteHandoffNotice({ handoff }: { handoff: SuiteRunHandoff }) {
  return (
    <p className="hint" role="note" aria-label="Suite handoff">
      Handed over from Suites: {handoff.entry} prepared against environment {handoff.environment} into prepared folder{" "}
      {handoff.prepared}
      {handoff.releases ? ` with release pins ${handoff.releases}` : " with no release pins"}. The run view selected{" "}
      {handoff.entry} and environment {handoff.environment} and preflights them again.{" "}
      {handoff.releases
        ? "It does not apply these release pins or read the prepared folder."
        : "It does not read the prepared folder."}
    </p>
  );
}
