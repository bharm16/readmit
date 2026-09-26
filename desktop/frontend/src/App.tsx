import { HelpTopics } from "./ContextHelp";
import { OperationAccess } from "./OperationAccess";
import { PrivacyDisclosure, SupportGuidance } from "./PrivacyDisclosure";
import { HubPanel } from "./HubPanel";
import { RunnerPanel } from "./RunnerPanel";
import { RunComparison } from "./RunComparison";
import { Baseline } from "./Baseline";
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
import { useLayoutEffect } from "react";
import {
  authorTest,
  buildReproducer,
  compareReproducers,
  cancel,
  compare,
  type CompareResult,
  createSampleWorkspace,
  createNamedProject,
  RequestScope,
  openNamedProject,
  listCatalog,
  locateItem,
  projectLocation,
  chooseProjectLocation,
  forgetProject,
  revealItem,
  saveItem,
  newIntentId,
  removeCaseFromProject,
  listNotes,
  saveNoteItem,
  listAttachments,
  addAttachments,
  removeAttachment,
  projectFiles,
  type SaveItemResult,
  type NoteItem,
  type Attachment,
  type ProjectFile,
  type CatalogItem,
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
  registerRevision,
  openWorkspace,
  buildIndex,
  describeIndex,
  type BuildIndexRequest,
  type IndexResult,
  captureSample,
  recordView,
  type EditorDraft,
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
  type Match,
  type ProjectOverviewResult,
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
import { MessageGrid, Report, Separator, Status } from "./shell";
import { CommandPalette, shortcut, type PaletteEntry } from "./CommandPalette";
import type { SortState } from "./DataTable";
import { sidebarOf, useRoutes, viewOf, type ReturnContext, type Route } from "./routes";
import { rootFontSize, useMeasured } from "./measure";
import {
  ICON_RAIL_REM,
  INSPECTOR_MAX_REM,
  INSPECTOR_MIN_REM,
  INSPECTOR_REM,
  LIST_MIN_REM,
  RAIL_BREAKPOINT_REM,
  SIDEBAR_REM,
} from "./geometry";
import { VocabularyContext } from "./vocabulary";
import { ImportPanel } from "./ImportPanel";
import { CapturePanel } from "./CapturePanel";
import { ObservationPanel } from "./ObservationPanel";
import { MaintenancePanel } from "./MaintenancePanel";
import { RawInspection } from "./RawInspection";
import { PerformanceCorpus } from "./PerformanceCorpus";
import type { CaptureObservationBinding } from "./bindings";
import { TaskPanel, TaskTabs } from "./TaskTabs";
import { IconButton } from "./IconButton";
import { DraftsToRestore, NewProjectSheet, ProjectList } from "./Projects";
import { CaseDetailsSheet, NoteSheet, ProjectSettingsSheet, RemoveCaseSheet, type ProjectSaveFailure } from "./CaseSheets";
import { AttachmentsList, FilesList, NotesList } from "./CaseContext";
import { CaseFilterSheet, CaseList, CaseSearchSheet, caseChoices, NO_VIEW, type CaseAction, type CaseView as CaseListView } from "./Cases";
import {
  BackLink,
  Categories,
  EmptyState,
  FormDialog,
  FrameContext,
  GLOBAL_DESTINATIONS,
  Modal,
  Menu,
  NavItem,
  OperationIndicator,
  PROJECT_DESTINATIONS,
  Page,
  ProjectSwitcher,
  ValueRows,
  folderName,
  humanize,
  type Destination,
} from "./layout";

/** How far one arrow key moves the details' edge, in rem. */
const INSPECTOR_STEP = 1;

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
type TestsView = "tests" | "suites";
type LibraryView = "checks" | "profiles" | "scenarios";
type SettingsView = "general" | "license" | "team" | "runners" | "security" | "storage";

const CASE_VIEWS: { key: CaseView; label: string }[] = [
  { key: "messages", label: "Messages" },
  { key: "timeline", label: "Timeline" },
  { key: "findings", label: "Findings" },
];
const TESTS_VIEWS: { key: TestsView; label: string }[] = [
  { key: "tests", label: "Tests" },
  { key: "suites", label: "Suites" },
];
const LIBRARY_VIEWS: { key: LibraryView; label: string }[] = [
  { key: "checks", label: "Checks" },
  { key: "profiles", label: "Profiles" },
  { key: "scenarios", label: "Scenarios" },
];
const TOOLS: { key: "inspect-file" | "sample-data" | "benchmarks"; label: string }[] = [
  { key: "inspect-file", label: "Inspect file" },
  { key: "sample-data", label: "Sample data" },
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

/** What the operation indicator says while one of this window's operations runs. */
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

  const [focused, setFocused] = useState<RegionId>("navigation");
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [theme, setTheme] = useState<Theme>("system");
  const [scale, setScale] = useState(0);
  const [editingGeneral, setEditingGeneral] = useState(false);
  // The inspector width each project was last given, in rem. The shown width
  // is clamped to the room there is now without overwriting the wider choice.
  const [inspectorWidths, setInspectorWidths] = useState<Record<string, number>>({});

  // Where the window is: one route, with the way back kept per destination.
  // Every destination stays mounted, so an unfinished edit survives looking at
  // another; only the shown one is drawn and announced.
  const { route, dispatch: routeTo } = useRoutes({ destination: "home" });
  const place = route.destination;
  const destination = sidebarOf(place);
  const currentRoute = useRef<Route>(route);
  currentRoute.current = route;
  const caseView = viewOf(route, "cases", CASE_VIEWS);
  const testsView = viewOf(route, "tests", TESTS_VIEWS);
  const libraryView = viewOf(route, "library", LIBRARY_VIEWS);
  const settingsView = viewOf(route, "settings", SETTINGS_VIEWS);
  const setView = useCallback((view: string) => routeTo({ type: "view", view }), [routeTo]);
  // What the place being left had, for Back to restore: the selection named,
  // and how far its page had been scrolled.
  const leaving = useCallback((selection?: string): ReturnContext => {
    const body = document.querySelector<HTMLElement>(`.page[data-page="${currentRoute.current.destination}"] .page-body`);
    return { ...(selection !== undefined ? { selection } : {}), ...(body ? { scrollTop: body.scrollTop } : {}) };
  }, []);
  // A route arrived at with a return context is where Back returned to: its
  // selection and scroll position come back with it.
  useLayoutEffect(() => {
    const returned = route.returnContext;
    if (!returned) return;
    if (returned.selection !== undefined) setSelectedCase(returned.selection);
    const body = document.querySelector<HTMLElement>(`.page[data-page="${route.destination}"] .page-body`);
    if (body && returned.scrollTop !== undefined) body.scrollTop = returned.scrollTop;
  }, [route]);
  const [caseFlow, setCaseFlow] = useState<CaseFlow | null>(null);
  const [fileDetails, setFileDetails] = useState(false);
  const [creatingProject, setCreatingProject] = useState(false);
  // The projects this viewer has opened, as the catalog lists them, and the
  // folder new projects go into.
  const [projects, setProjects] = useState<CatalogItem[]>([]);
  const [newProjectParent, setNewProjectParent] = useState<string | null>(null);
  const [createRefusedByLicense, setCreateRefusedByLicense] = useState(false);
  const [editingProject, setEditingProject] = useState(false);
  // The open project's cases, as the catalog lists them, and the search and
  // filters applied to them now; none of it is saved.
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [caseListView, setCaseListView] = useState<CaseListView>(NO_VIEW);
  const [caseListSort, setCaseListSort] = useState<SortState | null>(null);
  // The case selected in the list, by its catalog identity.
  const [selectedCase, setSelectedCase] = useState<string | null>(null);
  const listedCases = useRef<CatalogItem[]>([]);
  const [searchingCases, setSearchingCases] = useState(false);
  const [filteringCases, setFilteringCases] = useState(false);
  // The case task a row's menu started, with the case it is about.
  const [caseTask, setCaseTask] = useState<{ item: CatalogItem; action: CaseAction } | null>(null);
  // What the notes, attachments and files pages show, and the note being
  // written.
  const [notes, setNotes] = useState<NoteItem[]>([]);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [files, setFiles] = useState<ProjectFile[]>([]);
  const [noteEditing, setNoteEditing] = useState<{ note: NoteItem | null } | null>(null);
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
    const listed = await listCatalog({ context: { project: "", generation: 0 }, kind: "project", filter: {} });
    if (listed.page) setProjects(listed.page.items);
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
      // From outside every region, F6 starts at the first and Shift+F6 at the last.
      const current = regions.findIndex((region) => regionElements.current[region.id]?.contains(document.activeElement));
      const next =
        current < 0
          ? regions[direction > 0 ? 0 : regions.length - 1]
          : regions[(current + direction + regions.length) % regions.length];
      if (next) {
        focusRegion(next.id);
      }
    },
    [described, focusRegion],
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
          // A project opened is a new history: nothing selected, typed or
          // revealed in the last one comes with it.
          routeTo({ type: "project", projectId: result.workspace.root, to: { destination: "cases" } });
          setCaseFlow(null);
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
            // Opening records it among this viewer's projects, newest first.
            await openNamedProject(result.workspace.root);
          }
        } else {
          setOpenNotice(result);
        }
      });
      await refreshRecent();
      if (opened) {
        focusRegion("evidence");
      }
      return opened;
    },
    [clearWorkspace, focusRegion, refreshGuide, refreshRecent, run],
  );

  // Forgetting a recent folder writes the list, so it takes its turn like
  // every other write; what the list then holds is what the facade answers,
  // including when it refused.
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
          const at = currentRoute.current;
          if (at.destination === "cases" && at.objectId === name) {
            routeTo({ type: "view", view: "messages" });
          } else if (at.destination === "cases" && at.objectId !== undefined) {
            // One case is open at a time: the new one takes the old one's
            // place, and Back still leads to the list.
            routeTo({ type: "replace", to: { destination: "cases", objectId: name, view: "messages" } });
          } else {
            // Back returns to the list with this case selected.
            const listed = listedCases.current.find((item) => item.summary.case?.entry === name);
            routeTo({ type: "go", to: { destination: "cases", objectId: name, view: "messages" }, leaving: leaving(listed?.ref.id ?? name) });
          }
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
      routeTo({ type: "go", to: { destination: "run-test" }, leaving: leaving() });
      focusRegion("evidence");
    },
    [focusRegion, leaving, root, routeTo],
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
      routeTo({ type: "destination", destination: "cases", leaving: leaving() });
      focusRegion("evidence");
    },
    [focusRegion, leaving, routeTo, run],
  );

  // A new project is a named folder the application creates inside the
  // remembered parent. Once created, the window opens it on its empty Cases
  // view; a refusal keeps the sheet and what was typed.
  const createProjectNamed = useCallback(
    async (name: string) => {
      const answer = await createNamedProject({ name });
      const folder = answer.project?.summary.project?.folder;
      if (!folder) {
        // A refusal the license decides offers the way to activate one.
        setCreateRefusedByLicense(answer.state === "permission_denied");
        return { reason: answer.reason ?? "The project was not created." };
      }
      setCreatingProject(false);
      await openFolder(() => openWorkspace(folder));
      return undefined;
    },
    [openFolder],
  );

  // Continuing a retained draft opens its project and then the editor it
  // belongs to, where the draft comes back with its unsaved marker. Nothing
  // it describes is sent or applied.
  const resumeDraft = useCallback(
    async (draft: EditorDraft) => {
      if (draft.workspace !== root && !(await openFolder(() => openWorkspace(draft.workspace)))) return;
      const place = DRAFT_PLACES[draft.kind];
      if (!place) return;
      if (place.case && draft.case) {
        await verifyCase(draft.workspace, draft.case);
        setCaseFlow(place.case);
        return;
      }
      if (place.import) {
        setImporting(true);
        return;
      }
      if (place.observe) {
        setObserving(true);
        routeTo({ type: "go", to: { destination: "environments" } });
        return;
      }
      if (place.route) routeTo({ type: "go", to: place.route });
    },
    [openFolder, root, routeTo, verifyCase],
  );

  const openListedProject = useCallback(
    async (item: CatalogItem) => {
      const folder = item.summary.project?.folder;
      if (folder) await openFolder(() => openWorkspace(folder));
    },
    [openFolder],
  );

  // A refused project write — a license that no longer admits it, a change
  // the project cannot take — answers with its reason and no overview. The
  // window keeps the project as it last read it beside that refusal, so a
  // refusal never takes the project and its controls off the screen.
  const settleProject = useCallback((answer: ProjectOverviewResult) => {
    setInvestigation((held) => (answer.overview || !held?.overview ? answer : { ...answer, overview: held.overview }));
  }, []);

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
      routeTo({ type: "destination", destination: "cases", leaving: leaving() });
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
    if (currentRoute.current.objectId !== undefined) routeTo({ type: "back" });
    else routeTo({ type: "destination", destination: "cases" });
    focusRegion("evidence");
  }, [clearCase, focusRegion, routeTo]);

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
  // Every read of the open project carries its request context, so an answer
  // that arrives after the person moved to another project is dropped.
  const projectScope = useRef(new RequestScope());
  const projectContext = useCallback(() => projectScope.current.enter(root ?? ""), [root]);

  const refreshCases = useCallback(async () => {
    if (!root) {
      setCases([]);
      return;
    }
    const context = projectContext();
    const listed = await listCatalog({ context, kind: "case", filter: {} });
    if (!projectScope.current.current(listed)) return;
    setCases(listed.page?.items ?? []);
    listedCases.current = listed.page?.items ?? [];
  }, [projectContext, root]);

  // A case task from a row's menu. Those that belong to the open case's own
  // pages open it there; the rest open their sheet.
  const caseAction = useCallback(
    (item: CatalogItem, action: CaseAction) => {
      const entry = caseEntry(item);
      if ((action === "variant" || action === "compare") && root && entry) {
        void verifyCase(root, entry).then(() => setCaseFlow(action === "variant" ? "reproduce" : "compare"));
        return;
      }
      if (action === "details" && root && entry) {
        void verifyCase(root, entry).then(() => setFileDetails(true));
        return;
      }
      if (action === "notes" || action === "attachments") {
        routeTo({ type: "go", to: { destination: action === "notes" ? "case-notes" : "case-attachments", objectId: item.ref.id }, leaving: leaving(item.ref.id) });
        return;
      }
      setCaseTask({ item, action });
    },
    [leaving, root, routeTo, verifyCase],
  );

  // The open project as the catalog lists it: its name, settings and the
  // revision a save is based on.
  const currentProject = projects.find((item) => item.summary.project?.folder === root) ?? null;
  const contextCase = cases.find((item) => item.ref.id === route.objectId) ?? null;

  // What a refused save says, at the field it names.
  const saveFailure = (answer: SaveItemResult, fields: Record<string, string>) => {
    const problem = answer.problems[0];
    if (answer.outcome === "conflict") return { reason: answer.reason ?? "This was changed elsewhere since you opened it. Close and open it again." };
    return {
      reason: answer.problems.map((entry) => entry.problem).join(" ") || answer.reason || "Not saved.",
      ...(problem && fields[problem.field] ? { field: fields[problem.field] } : {}),
    };
  };

  const refreshNotes = useCallback(async () => {
    const context = projectContext();
    const caseRef = place === "case-notes" ? contextCase?.ref : undefined;
    const answer = await listNotes({ context, ...(caseRef ? { case: caseRef } : {}) });
    if (projectScope.current.current(answer)) setNotes(answer.notes);
  }, [contextCase, place, projectContext]);

  const refreshAttachments = useCallback(async () => {
    if (!contextCase) return;
    const answer = await listAttachments({ context: projectContext(), ref: contextCase.ref });
    if (projectScope.current.current(answer)) setAttachments(answer.attachments);
  }, [contextCase, projectContext]);

  useEffect(() => {
    if (place === "notes" || place === "case-notes") void refreshNotes();
    if (place === "case-attachments") void refreshAttachments();
    if (place === "project-files" && root) {
      void projectFiles({ context: projectContext(), ref: currentProject?.ref ?? { kind: "project", id: "" } }).then((answer) => {
        if (projectScope.current.current(answer)) setFiles(answer.files);
      });
    }
  }, [currentProject, place, projectContext, refreshAttachments, refreshNotes, root]);

  useEffect(() => {
    setCaseListView(NO_VIEW);
    setCaseListSort(null);
    setCaseTask(null);
    void refreshCases();
  }, [refreshCases]);

  const refreshListing = useCallback(async () => {
    if (!root) return;
    const result = await openWorkspace(root);
    if (result.workspace) {
      setWorkspace(result);
    }
    await refreshCases();
  }, [refreshCases, root]);

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


  // Moves to a sidebar destination, back where it was last left. Focus goes
  // to the page, so a keyboard user lands where the page now is.
  const go = useCallback(
    (to: Destination) => {
      routeTo({ type: "destination", destination: to, leaving: leaving() });
      focusRegion("evidence");
    },
    [focusRegion, leaving, routeTo],
  );

  // Opens a place reached from inside another: a child destination, or a
  // destination at one of its views.
  const open = useCallback(
    (to: Route) => {
      routeTo({ type: "go", to, leaving: leaving() });
      focusRegion("evidence");
    },
    [focusRegion, leaving, routeTo],
  );

  const back = useCallback(() => {
    routeTo({ type: "back" });
    focusRegion("evidence");
  }, [focusRegion, routeTo]);

  const openSettings = useCallback((view: SettingsView) => open({ destination: "settings", view }), [open]);

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
  //
  // Whether a command applies where the person is decides once, in
  // available(), both what the palette lists and whether perform() runs it;
  // the actions themselves carry no guards of their own.
  const actions: Record<CommandId, () => void> = {
    "command-palette": () => setPaletteOpen(true),
    "search-workspace": () => setSearching(true),
    "new-project": () => {
      setCreatingProject(true);
      void projectLocation().then((answer) => setNewProjectParent(answer.location ?? null));
    },
    "open-workspace": () => void openFolder(selectWorkspace),
    "create-sample-workspace": () => void openFolder(createSampleWorkspace),
    "open-project": () => setEditingProject(true),
    "create-test": () => {
      go("cases");
      setCaseFlow("test");
    },
    "create-report": () => go("reports"),
    "manage-profiles": () => open({ destination: "library", view: "profiles" }),
    "maintain-workspace": () => openStorage("backup"),
    "check-staged-upgrade": () => openStorage("upgrade"),
    "manage-scenarios": () => open({ destination: "library", view: "scenarios" }),
    "manage-assertions": () => open({ destination: "library", view: "checks" }),
    "inspect-raw-file": () => {
      open({ destination: "inspect-file" });
      setRawRequest((count) => count + 1);
    },
    "performance-corpus": () => {
      open({ destination: "benchmarks" });
      setCorpusRequest((count) => count + 1);
    },
    "cancel-operation": () => cancel(),
    "next-region": () => step(1),
    "previous-region": () => step(-1),
    "go-to-navigation": () => focusRegion("navigation"),
    "go-to-evidence": () => focusRegion("evidence"),
    "go-to-inspector": () => focusRegion("inspector"),
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
  // Escape is not among them: it closes the topmost dialog or menu, and never
  // cancels work that is running.
  const latest = useRef(actions);
  useEffect(() => {
    latest.current = actions;
  });
  // Runs a command only where it applies; see available() below.
  const perform = (id: CommandId) => {
    if (available(id)) latest.current[id]();
  };
  const performLatest = useRef(perform);
  performLatest.current = perform;

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const chosen = ((): CommandId | null => {
        if (event.key === "F6") {
          return event.shiftKey ? "previous-region" : "next-region";
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
        performLatest.current(chosen);
      }
    };
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, []);

  const artifacts = opened?.artifacts ?? [];
  const named = (kind: string) => artifacts.filter((artifact) => artifact.kind === kind).map((artifact) => artifact.name);
  const projectName = overview?.title || (root ? folderName(root) : "");
  const caseTitle = verified ? overview?.cases.find((entry) => entry.name === verified.name)?.title || verified.name : "";
  const subpage = importing && root ? "import" : capturing && root ? "capture" : null;
  const inspecting = selectedOccurrence !== null || inspectionResult !== null || running === "inspect";
  const detailsShown = place === "cases" && verified !== null && subpage === null && inspecting;
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

  // The window's own width decides the sidebar: a labelled 13rem sidebar where
  // there is room, an icon rail below that with the project switcher moved
  // into the page header, never both gone.
  const [frame, setFrame] = useState<HTMLDivElement | null>(null);
  const { width: frameWidth, rem } = useMeasured(frame);
  const compact = frameWidth > 0 && frameWidth / rem < RAIL_BREAKPOINT_REM;
  const switcher = root ? (
    <ProjectSwitcher
      name={projectName}
      recent={recentProjects(projects, root)}
      disabled={busy}
      onOpenRecent={(folder) => void openFolder(() => openWorkspace(folder))}
      onOpen={() => perform("open-workspace")}
      onNew={() => perform("new-project")}
      onSettings={() => perform("open-project")}
      onFiles={() => open({ destination: "project-files" })}
    />
  ) : null;

  // The inspector keeps the width chosen for this project, clamped to the room
  // there is now. When the list beside it would fall below its useful width,
  // the selection is shown on its own with the way back to the list.
  const sidebarRem = compact ? ICON_RAIL_REM : SIDEBAR_REM;
  const workareaRem = frameWidth > 0 ? frameWidth / rem - sidebarRem : Number.POSITIVE_INFINITY;
  const preferredInspector = (root !== null ? inspectorWidths[root] : undefined) ?? INSPECTOR_REM;
  const inspectorRem = Math.max(INSPECTOR_MIN_REM, Math.min(preferredInspector, INSPECTOR_MAX_REM, workareaRem - LIST_MIN_REM - 1 / rem));
  const detailOnly = detailsShown && workareaRem - inspectorRem - 1 / rem < LIST_MIN_REM;

  // Every region the facade declares has an element here, for the same reason
  // every command has an action: a region the window forgot is a type error.
  // ReactElement rather than ReactNode, because ReactNode admits null and would
  // accept a region entered as nothing. What a region then draws is beyond it.
  const content: Record<RegionId, ReactElement> = {
    navigation: (
      <>
        <ul className="nav-list">
          <NavItem id="home" label="Projects" current={destination === "home"} onSelect={go} />
        </ul>
        {root && !compact ? <div className="sidebar-switcher">{switcher}</div> : null}
        {root ? (
          <ul className="nav-list">
            {PROJECT_DESTINATIONS.map((item) => (
              <NavItem key={item.id} id={item.id} label={item.label} current={destination === item.id} onSelect={go} />
            ))}
          </ul>
        ) : null}
        <ul className="nav-list nav-footer">
          {GLOBAL_DESTINATIONS.map((item) => (
            <NavItem key={item.id} id={item.id} label={item.label} current={destination === item.id} onSelect={go} />
          ))}
        </ul>
        {busy ? (
          <OperationIndicator label={running ? PROGRESS[running] : "Working…"} onStop={interruptible ? () => cancel() : undefined} />
        ) : null}
      </>
    ),
    evidence: (
      <>
        <Page
          id="home"
          shown={place === "home"}
          title="Projects"
          actions={
            <>
              <button type="button" disabled={busy} onClick={() => perform("open-workspace")}>
                Open
              </button>
              <button type="button" className="primary" disabled={busy} onClick={() => perform("new-project")}>
                New project
              </button>
            </>
          }
        >
          <DraftsToRestore
            drafts={drafts ?? []}
            projectName={(draft) => projects.find((item) => item.summary.project?.folder === draft.workspace)?.name ?? folderName(draft.workspace)}
            onResume={(draft) => void resumeDraft(draft)}
            onDiscard={(draft) => dropDraft(draft.id)}
          />
          {openNotice ? (
            <div className="notice danger">
              <Report indicators={indicators} progress={null} result={openNotice} />
              <button type="button" disabled={busy} onClick={() => perform("open-workspace")}>
                Choose another folder…
              </button>
            </div>
          ) : null}
          <ProjectList
            projects={projects}
            busy={busy}
            onOpen={(item) => void openListedProject(item)}
            onLocate={(item) => void locateItem({ context: { project: "", generation: 0 }, ref: item.ref }).then(refreshRecent)}
            onSettings={(item) => void openListedProject(item).then(() => setEditingProject(true))}
            onReveal={(item) => void revealItem({ context: { project: "", generation: 0 }, ref: item.ref })}
            onForget={(item) => void forgetProject(item.ref.id).then(refreshRecent)}
            onNew={() => setCreatingProject(true)}
          />
          <div className="quiet-action">
            <button type="button" className="quiet" disabled={busy} onClick={() => perform("create-sample-workspace")}>
              Try demo
            </button>
          </div>
        </Page>

        <Page
          id="cases"
          shown={place === "cases"}
          back={
            subpage !== null ? (
              <BackLink label="Cases" onBack={leaveSubpage} />
            ) : verified && caseFlow !== null ? (
              <BackLink label={caseTitle} name={caseTitle} onBack={() => setCaseFlow(null)} />
            ) : verified ? (
              <BackLink label="Cases" onBack={backToProject} />
            ) : null
          }
          title={
            subpage === "import"
              ? "Import evidence"
              : subpage === "capture"
                ? "Capture"
                : verified
                    ? caseFlow !== null
                      ? CASE_FLOW_TITLES[caseFlow]
                      : caseTitle
                    : "Cases"
          }
          actions={
            root && subpage === null && !verified ? (
              <>
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
                <Menu
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
          {capturing && root ? (
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
                go("environments");
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

          {root && subpage === null && verified === null ? (
            <>
              {caseNotice ? (
                <div className="notice danger">
                  <Report indicators={indicators} progress={null} result={caseNotice} />
                </div>
              ) : null}
              <div className="toolbar page-toolbar">
                <IconButton icon="search" label="Search cases" onClick={() => setSearchingCases(true)} />
                <IconButton icon="filter" label="Filter cases" onClick={() => setFilteringCases(true)} />
              </div>
              <CaseList
                cases={cases}
                view={caseListView}
                onView={setCaseListView}
                selected={selectedCase}
                onSelect={setSelectedCase}
                onOpen={(item) => {
                  const entry = caseEntry(item);
                  if (entry) void verifyCase(root, entry);
                }}
                onAction={caseAction}
                onRetry={() => void refreshCases()}
                onLocate={(item) => void locateItem({ context: projectContext(), ref: item.ref }).then(refreshCases)}
                onImport={() => setImporting(true)}
                sort={caseListSort}
                onSort={setCaseListSort}
                busy={busy}
              />
            </>
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
              <TaskTabs label="Case views" id="case-views" tabs={CASE_VIEWS} selected={caseView} onSelect={setView} keepMounted>
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
                    onManageProfiles={() => perform("manage-profiles")}
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
                    <EmptyState
                      title="Messages are not loaded yet"
                      action={
                        <button type="button" onClick={() => { setCaseFlow(null); setView("messages"); }}>
                          Open messages
                        </button>
                      }
                    />
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
                    <EmptyState
                      title="Messages are not loaded yet"
                      action={
                        <button type="button" onClick={() => { setCaseFlow(null); setView("messages"); }}>
                          Open messages
                        </button>
                      }
                    />
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

        <Page
          id="tests"
          shown={place === "tests"}
          title="Tests"
          actions={
            root ? (
              <>
                <button type="button" onClick={() => open({ destination: "library" })}>
                  Library
                </button>
                <Menu label="More test actions" items={[{ label: "Baselines", onSelect: () => open({ destination: "baselines" }) }]} />
              </>
            ) : null
          }
        >
          {root ? (
            <TaskTabs label="Test views" id="tests-views" tabs={TESTS_VIEWS} selected={testsView} onSelect={setView} keepMounted>
              <TaskPanel tabs="tests-views" tab="tests" className="task-panel view-panel" shown={testsView === "tests"}>
                <CanonicalTestEditor key={"editor-" + root} workspace={root} drafts={drafts} busy={busy} />
              </TaskPanel>
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
            </TaskTabs>
          ) : (
            noProject("tests")
          )}
        </Page>

        <Page id="library" shown={place === "library"} title="Library" back={<BackLink label="Tests" onBack={back} />}>
          {root ? (
            <TaskTabs label="Library views" id="library-views" tabs={LIBRARY_VIEWS} selected={libraryView} onSelect={setView} keepMounted>
              <TaskPanel tabs="library-views" tab="checks" className="task-panel view-panel" shown={libraryView === "checks"}>
                <AssertionSetAuthoring key={"assertions-" + root} workspace={root} drafts={drafts} busy={busy} inspected={inspected} />
              </TaskPanel>
              <TaskPanel tabs="library-views" tab="profiles" className="task-panel view-panel" shown={libraryView === "profiles"}>
                <ProfileEditor key={`profile-${root}`} workspace={root} drafts={drafts} busy={busy} />
              </TaskPanel>
              <TaskPanel tabs="library-views" tab="scenarios" className="task-panel view-panel" shown={libraryView === "scenarios"}>
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
            </TaskTabs>
          ) : (
            noProject("library")
          )}
        </Page>

        <Page id="baselines" shown={place === "baselines"} title="Baselines" back={<BackLink label="Tests" onBack={back} />}>
          {root ? <Baseline key={root} workspace={root} busy={busy} onSaved={() => void refreshListing()} /> : noProject("baselines")}
        </Page>

        <Page
          id="runs"
          shown={place === "runs"}
          title="Runs"
          actions={
            root ? (
              <>
                <button type="button" onClick={() => open({ destination: "compare-runs" })}>
                  Compare
                </button>
                <button type="button" className="primary" onClick={() => open({ destination: "run-test" })}>
                  Run test
                </button>
              </>
            ) : null
          }
        >
          {root ? <RunExplanation key={"explain-" + root} workspace={root} entries={artifacts} busy={busy} /> : noProject("runs")}
        </Page>

        <Page id="run-test" shown={place === "run-test"} title="Run test" back={<BackLink label="Runs" onBack={back} />}>
          {root ? (
            <>
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
            </>
          ) : (
            noProject("runs")
          )}
        </Page>

        <Page id="compare-runs" shown={place === "compare-runs"} title="Compare runs" back={<BackLink label="Runs" onBack={back} />}>
          {root ? <RunComparison key={"runs-" + root} workspace={root} busy={busy} entries={artifacts} /> : noProject("runs")}
        </Page>

        <Page
          id="environments"
          shown={place === "environments"}
          title={observing && root ? "Observations" : "Environments"}
          back={
            observing && root ? (
              <BackLink
                label="Environments"
                onBack={() => {
                  setObserving(false);
                  setCaptureBinding(null);
                }}
              />
            ) : null
          }
          actions={
            root && !observing ? (
              <button type="button" disabled={busy} onClick={() => setObserving(true)}>
                Observations
              </button>
            ) : null
          }
        >
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
          ) : opened ? (
            <EnvironmentPanel
              workspace={root ?? ""}
              targetFile="targets/default.json"
              secretsFile="secrets.json"
              policyFile="send-policy.json"
              planFile="reset-plan.json"
              initialTab="target"
              drafts={drafts}
              onPlanSaved={() => void refreshListing()}
            />
          ) : (
            noProject("environments")
          )}
        </Page>

        <Page
          id="reports"
          shown={place === "reports"}
          title="Reports"
          actions={
            root ? (
              <>
                <button type="button" onClick={() => open({ destination: "share-report" })}>
                  Share
                </button>
                <Menu
                  label="More report actions"
                  items={[
                    { label: "Transform and export", onSelect: () => open({ destination: "export-report" }) },
                  ]}
                />
              </>
            ) : null
          }
        >
          {root ? <PacketPanel workspace={root} entries={artifacts} onRefresh={() => void refreshListing()} /> : noProject("reports")}
        </Page>

        <Page id="share-report" shown={place === "share-report"} title="Share report" back={<BackLink label="Reports" onBack={back} />}>
          {root ? <PrivacyPanel workspace={root} entries={artifacts} drafts={drafts} onRefresh={() => void refreshListing()} /> : noProject("reports")}
        </Page>

        <Page id="export-report" shown={place === "export-report"} title="Transform and export" back={<BackLink label="Reports" onBack={back} />}>
          {root ? (
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
          ) : (
            noProject("reports")
          )}
        </Page>

        <Page
          id="notes"
          shown={place === "notes"}
          title="Notes"
          back={<BackLink label="Cases" onBack={back} />}
          actions={
            root ? (
              <button type="button" className="primary" onClick={() => setNoteEditing({ note: null })}>
                New note
              </button>
            ) : null
          }
        >
          {root ? <NotesList notes={notes} onEdit={(note) => setNoteEditing({ note })} onNew={() => setNoteEditing({ note: null })} /> : noProject("notes")}
        </Page>

        <Page
          id="case-notes"
          shown={place === "case-notes"}
          title="Notes"
          back={<BackLink label={contextCase?.name ?? "Cases"} name={contextCase?.name ?? "cases"} onBack={back} />}
          actions={
            contextCase ? (
              <button type="button" className="primary" onClick={() => setNoteEditing({ note: null })}>
                New note
              </button>
            ) : null
          }
        >
          {root ? <NotesList notes={notes} onEdit={(note) => setNoteEditing({ note })} onNew={() => setNoteEditing({ note: null })} /> : noProject("notes")}
        </Page>

        <Page
          id="case-attachments"
          shown={place === "case-attachments"}
          title="Attachments"
          back={<BackLink label={contextCase?.name ?? "Cases"} name={contextCase?.name ?? "cases"} onBack={back} />}
          actions={
            contextCase && attachments.length > 0 ? (
              <button
                type="button"
                className="primary"
                disabled={busy}
                onClick={() => void addAttachments({ context: projectContext(), ref: contextCase.ref }).then((answer) => answer.state !== "cancelled" && setAttachments(answer.attachments))}
              >
                Add attachment
              </button>
            ) : null
          }
        >
          {root && contextCase ? (
            <AttachmentsList
              attachments={attachments}
              busy={busy}
              onAdd={() => void addAttachments({ context: projectContext(), ref: contextCase.ref }).then((answer) => answer.state !== "cancelled" && setAttachments(answer.attachments))}
              onRemove={(file) => void removeAttachment({ context: projectContext(), case: contextCase.ref, id: file.id }).then((answer) => setAttachments(answer.attachments))}
            />
          ) : (
            noProject("attachments")
          )}
        </Page>

        <Page id="project-files" shown={place === "project-files"} title="Files" back={<BackLink label="Cases" onBack={back} />}>
          {root ? (
            <FilesList
              files={files}
              onOpen={() => {
                open({ destination: "inspect-file" });
                setRawRequest((count) => count + 1);
              }}
            />
          ) : (
            noProject("files")
          )}
        </Page>

        <Page id="tools" shown={place === "tools"} title="Tools">
          <ul className="launcher" aria-label="Tools">
            {TOOLS.map((tool) => (
              <li key={tool.key}>
                <button
                  type="button"
                  className="launcher-row"
                  onClick={() => {
                    open({ destination: tool.key });
                    if (tool.key === "inspect-file") setRawRequest((count) => count + 1);
                    if (tool.key === "benchmarks") setCorpusRequest((count) => count + 1);
                  }}
                >
                  <span>{tool.label}</span>
                  <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
                    <path d="M6 3l5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </button>
              </li>
            ))}
          </ul>
        </Page>

        <Page id="inspect-file" shown={place === "inspect-file"} title="Inspect file" back={<BackLink label="Tools" onBack={back} />}>
          <RawInspection busy={busy} indicators={indicators} request={rawRequest} />
        </Page>

        <Page id="sample-data" shown={place === "sample-data"} title="Sample data" back={<BackLink label="Tools" onBack={back} />}>
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
              onCreateSample={() => perform("create-sample-workspace")}
              onOpenCase={(name: string) => {
                if (root) void verifyCase(root, name);
              }}
              onRun={(trial, output) => void practise(trial, output)}
              onCancel={cancelRunning}
              onCapture={(output) => void importSample(output)}
            />
          ) : null}
          <EmptyState
            title="Synthetic demo project"
            action={
              <button type="button" className="primary" disabled={busy} onClick={() => perform("create-sample-workspace")}>
                Try demo
              </button>
            }
          />
        </Page>

        <Page id="benchmarks" shown={place === "benchmarks"} title="Benchmarks" back={<BackLink label="Tools" onBack={back} />}>
          <PerformanceCorpus busy={busy} indicators={indicators} request={corpusRequest} />
        </Page>

        <Page id="settings" shown={place === "settings"} title="Settings">
          <Categories label="Settings categories" categories={SETTINGS_VIEWS} selected={settingsView} onSelect={setView}>
            <TaskPanel tabs="settings-views" tab="general" className="task-panel view-panel" shown={settingsView === "general"}>
              <div className="section-header">
                <h2>General</h2>
                <button type="button" onClick={() => setEditingGeneral(true)}>
                  Edit
                </button>
              </div>
              <ValueRows
                label="General"
                rows={[
                  { label: "Theme", value: themeLabel(theme) },
                  { label: "Text size", value: `${described?.text_scales[scale] ?? 100}%` },
                ]}
              />
              <GeneralEditor
                open={editingGeneral}
                themes={described?.themes ?? ["system"]}
                scales={described?.text_scales ?? [100]}
                theme={theme}
                scale={scale}
                onClose={() => setEditingGeneral(false)}
                onSave={(nextTheme, nextScale) => {
                  setTheme(nextTheme);
                  setScale(nextScale);
                  setEditingGeneral(false);
                }}
              />
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
                  onOpenRunPanel={() => open({ destination: "run-test" })}
                  onStartCapture={() => {
                    setCapturing(true);
                    open({ destination: "cases" });
                  }}
                  onStartObservation={() => {
                    setCaptureBinding(null);
                    setObserving(true);
                    open({ destination: "environments" });
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
          </Categories>
        </Page>

        <Page id="help" shown={place === "help"} title="Help">
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
        <div className="details-header">
          {detailOnly ? <BackLink label="Messages" onBack={closeDetails} /> : null}
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
  };

  // What the palette lists: the actions of where the person is first, then
  // the destinations. It never lists itself, a project action with no project
  // open, or Cancel while nothing cancellable runs.
  function available(id: CommandId): boolean {
    switch (id) {
      case "command-palette":
        return !paletteOpen;
      case "new-project":
      case "open-workspace":
      case "create-sample-workspace":
        return !busy;
      case "cancel-operation":
        return busy && interruptible;
      case "create-test":
        return root !== null && verified !== null;
      case "go-to-inspector":
        return detailsShown;
      case "open-project":
        return !busy && root !== null;
      case "search-workspace":
      case "create-report":
      case "manage-profiles":
      case "manage-scenarios":
      case "manage-assertions":
      case "maintain-workspace":
      case "check-staged-upgrade":
        return root !== null;
      default:
        return true;
    }
  }
  const contextual: CommandId[] = ["cancel-operation", "create-test", "create-report", "search-workspace"];
  const paletteEntries: PaletteEntry[] = [
    ...(described?.commands ?? [])
      // The palette never lists itself.
      .filter((command) => command.id !== "command-palette" && available(command.id))
      .sort((a, b) => Number(contextual.includes(b.id)) - Number(contextual.includes(a.id)))
      .map((command) => ({
        id: command.id,
        label:
          command.id === "cancel-operation" && running
            ? `Cancel ${PROGRESS[running].replace(/…$/, "").replace(/^./, (first) => first.toLowerCase())}`
            : command.title,
        ...(command.keys ? { keys: shortcut(command.keys) } : {}),
        run: () => perform(command.id),
      })),
    ...[{ id: "home" as Destination, label: "Projects" }, ...(root ? PROJECT_DESTINATIONS : []), ...GLOBAL_DESTINATIONS].map((item) => ({
      id: `destination-${item.id}`,
      label: item.label,
      run: () => go(item.id),
    })),
  ];

  if (!described) {
    return (
      <main className="starting">
        <h1>Readmit</h1>
        <Status indicator={indicators.get(windowState)} state={windowState} reason={windowReason} />
      </main>
    );
  }

  // Each region the facade declares, drawn where it sits: the sidebar holds
  // navigation, the work area the page and the details of a selection. The
  // order they are drawn in is the order the facade declares, which is the
  // order Tab and F6 move through them.
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

  const panes = { "--inspector-width": `${inspectorRem}rem` } as CSSProperties;

  return (
    <IndicatorsContext.Provider value={indicators}>
      <VocabularyContext.Provider value={described.vocabulary}>
        <FrameContext.Provider value={{ compact, switcher }}>
          <div className={compact ? "app compact" : "app"} ref={setFrame}>
            <nav className="sidebar" aria-label="Main">
              {region("navigation")}
            </nav>
            <div className={`workarea${detailsShown ? (detailOnly ? " detail-only" : " with-details") : ""}`} style={panes}>
              {region("evidence", detailOnly)}
              {detailsShown && !detailOnly ? (
                <Separator
                  value={inspectorRem}
                  min={INSPECTOR_MIN_REM}
                  max={INSPECTOR_MAX_REM}
                  step={INSPECTOR_STEP}
                  onChange={(width) => {
                    if (root !== null) setInspectorWidths((held) => ({ ...held, [root]: width }));
                  }}
                  edge={() => regionElements.current.inspector?.getBoundingClientRect().right ?? null}
                  rem={rootFontSize}
                />
              ) : null}
              {region("inspector", !detailsShown)}
            </div>
          </div>
        </FrameContext.Provider>

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

        <CaseSearchSheet
          open={searchingCases}
          query={caseListView.query}
          onApply={(query) => setCaseListView({ ...caseListView, query })}
          onClose={() => setSearchingCases(false)}
        />
        <CaseFilterSheet
          open={filteringCases}
          view={caseListView}
          {...caseChoices(cases)}
          onApply={setCaseListView}
          onClose={() => setFilteringCases(false)}
        />

        {currentProject ? (
          <ProjectSettingsSheet
            open={editingProject}
            details={{
              name: currentProject.name,
              owner: currentProject.summary.project?.owner ?? "",
              tags: currentProject.summary.project?.tags ?? [],
              revisions: currentProject.summary.project?.revisions ?? [],
            }}
            folder={currentProject.summary.project?.folder ?? ""}
            onReveal={() => void revealItem({ context: projectContext(), ref: currentProject.ref })}
            onNotes={() => {
              setEditingProject(false);
              open({ destination: "notes" });
            }}
            onSave={async (details, reassign): Promise<ProjectSaveFailure | void> => {
              const answer = await saveItem({
                context: projectContext(),
                kind: "project",
                item: currentProject.ref.id,
                ...(currentProject.ref.revision ? { base_revision: currentProject.ref.revision } : {}),
                intent_id: newIntentId(),
                draft: {
                  project: {
                    name: details.name,
                    ...(details.owner ? { owner: details.owner } : {}),
                    tags: details.tags,
                    revisions: details.revisions,
                    reassign: Object.entries(reassign).map(([from, to]) => ({ from, ...(to ? { to } : {}) })),
                  },
                },
              });
              if (answer.outcome === "saved") {
                setEditingProject(false);
                await refreshRecent();
                return;
              }
              const referring = answer.problems
                .filter((problem) => problem.referring && problem.referring.length > 0)
                .map((problem) => ({ revision: problem.field.replace(/^revisions\./, ""), cases: problem.referring!.map((entry) => entry.name) }));
              return { ...saveFailure(answer, { name: "project-name", owner: "project-owner" }), ...(referring.length > 0 ? { referring } : {}) };
            }}
            onClose={() => setEditingProject(false)}
          />
        ) : null}

        {caseTask?.action === "edit" ? (
          <CaseDetailsSheet
            open
            details={{
              name: caseTask.item.name,
              status: caseTask.item.summary.case?.status ?? "open",
              owner: caseTask.item.summary.case?.owner ?? "",
              tags: caseTask.item.summary.case?.tags ?? [],
              revision: caseTask.item.summary.case?.interface_revision ?? "",
              incidents: caseTask.item.summary.case?.incidents ?? [],
            }}
            revisions={currentProject?.summary.project?.revisions ?? []}
            onSave={async (details) => {
              const answer = await saveItem({
                context: projectContext(),
                kind: "case",
                item: caseTask.item.ref.id,
                ...(caseTask.item.ref.revision ? { base_revision: caseTask.item.ref.revision } : {}),
                intent_id: newIntentId(),
                draft: {
                  case: {
                    name: details.name,
                    status: details.status,
                    ...(details.owner ? { owner: details.owner } : {}),
                    tags: details.tags,
                    ...(details.revision ? { interface_revision: details.revision } : {}),
                    incidents: details.incidents,
                  },
                },
              });
              if (answer.outcome === "saved") {
                setCaseTask(null);
                await refreshCases();
                return;
              }
              return saveFailure(answer, { name: "case-name", owner: "case-owner", status: "case-status" });
            }}
            onClose={() => setCaseTask(null)}
          />
        ) : null}

        {caseTask?.action === "remove" ? (
          <RemoveCaseSheet
            open
            name={caseTask.item.name}
            onRemove={async () => {
              const answer = await removeCaseFromProject({ context: projectContext(), ref: caseTask.item.ref });
              if (answer.state !== "completed") return { reason: answer.reason ?? "The case was not removed." };
              setCaseTask(null);
              await refreshCases();
            }}
            onClose={() => setCaseTask(null)}
          />
        ) : null}

        {noteEditing ? (
          <NoteSheet
            open
            note={noteEditing.note ? { id: noteEditing.note.id, name: noteEditing.note.name, content: noteEditing.note.content } : { name: "", content: "" }}
            related={place === "case-notes" && contextCase ? contextCase.name : currentProject?.name ?? "This project"}
            onSave={async (note) => {
              const caseRef = place === "case-notes" ? contextCase?.ref : noteEditing.note?.case;
              const answer = await saveNoteItem({
                context: projectContext(),
                intent_id: newIntentId(),
                note: { ...(note.id ? { id: note.id } : {}), name: note.name, content: note.content, ...(caseRef ? { case: caseRef } : {}) },
              });
              if (answer.state !== "completed") return { reason: answer.reason ?? "The note was not saved.", field: "note-name" };
              setNoteEditing(null);
              await refreshNotes();
            }}
            onClose={() => setNoteEditing(null)}
          />
        ) : null}

        <NewProjectSheet
          open={creatingProject}
          location={newProjectParent}
          onChangeLocation={async () => {
            const chosen = await chooseProjectLocation();
            if (chosen.location) setNewProjectParent(chosen.location);
          }}
          onCreate={createProjectNamed}
          onActivate={
            createRefusedByLicense
              ? () => {
                  setCreatingProject(false);
                  openSettings("license");
                }
              : undefined
          }
          onClose={() => {
            setCreatingProject(false);
            setCreateRefusedByLicense(false);
          }}
        />

        <CommandPalette open={paletteOpen} entries={paletteEntries} onClose={() => setPaletteOpen(false)} />
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

function themeLabel(theme: Theme): string {
  return theme === "system" ? "System" : theme === "light" ? "Light" : theme === "dark" ? "Dark" : humanize(theme);
}

/** General settings' Edit sheet: the saved choices, prefilled; Save applies
 * them together. */
function GeneralEditor({
  open,
  themes,
  scales,
  theme,
  scale,
  onClose,
  onSave,
}: {
  open: boolean;
  themes: Theme[];
  scales: number[];
  theme: Theme;
  scale: number;
  onClose: () => void;
  onSave: (theme: Theme, scale: number) => void;
}) {
  const [draftTheme, setDraftTheme] = useState(theme);
  const [draftScale, setDraftScale] = useState(scale);
  useEffect(() => {
    if (open) {
      setDraftTheme(theme);
      setDraftScale(scale);
    }
  }, [open, theme, scale]);
  return (
    <FormDialog
      open={open}
      title="General"
      size="small"
      submitLabel="Save"
      dirty={draftTheme !== theme || draftScale !== scale}
      onClose={onClose}
      onSubmit={() => onSave(draftTheme, draftScale)}
    >
      <label htmlFor="theme">Theme</label>
      <select id="theme" value={draftTheme} onChange={(event) => setDraftTheme(event.target.value as Theme)}>
        {themes.map((choice) => (
          <option key={choice} value={choice}>
            {themeLabel(choice)}
          </option>
        ))}
      </select>
      <label htmlFor="text-size">Text size</label>
      <select id="text-size" value={draftScale} onChange={(event) => setDraftScale(Number(event.target.value))}>
        {scales.map((percent, index) => (
          <option key={percent} value={index}>
            {percent}%
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** The recent projects the switcher offers besides the open one, at most
 * five, by the names their projects record. */
function recentProjects(projects: CatalogItem[], open: string): { key: string; name: string }[] {
  return projects
    .filter((item) => item.availability === "available" && item.summary.project?.folder !== open)
    .slice(0, 5)
    .map((item) => ({ key: item.summary.project?.folder ?? "", name: item.name }));
}

/** The entry a listed case's bundle is at inside its project, which the
 * message reader opens it by. */
function caseEntry(item: CatalogItem): string | undefined {
  return item.summary.case?.entry;
}

/** Where each kind of retained draft is continued. */
const DRAFT_PLACES: Record<string, { route?: Route; case?: CaseFlow; import?: true; observe?: true }> = {
  "canonical-test": { route: { destination: "tests", view: "tests" } },
  suite: { route: { destination: "tests", view: "suites" } },
  "assertion-set-draft": { route: { destination: "library", view: "checks" } },
  "local-profile": { route: { destination: "library", view: "profiles" } },
  note: { route: { destination: "notes" } },
  "redact-policy": { route: { destination: "share-report" } },
  "redact-inventory": { route: { destination: "share-report" } },
  target: { route: { destination: "environments" } },
  "test-draft": { case: "test" },
  "reproducer-plan": { case: "reproduce" },
  import: { import: true },
  "observation-source": { observe: true },
  "observation-window": { observe: true },
};
