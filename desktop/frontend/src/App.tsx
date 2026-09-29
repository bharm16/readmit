import { HelpTopics } from "./ContextHelp";
import { OperationAccess } from "./OperationAccess";
import { SupportGuidance } from "./SupportGuidance";
import { GeneralView, SecurityView, usePreferences, type ConnectionRoute } from "./Settings";
import { useEncryption } from "./Encryption";
import { HubPanel } from "./HubPanel";
import { RunnerPanel } from "./RunnerPanel";
import { RunComparison } from "./RunComparison";
import { RunPanel } from "./RunPanel";
import { RunExplanation } from "./RunExplanation";
import { ReplayPanel } from "./ReplayPanel";
import { PacketPanel } from "./PacketPanel";
import { PrivacyPanel } from "./PrivacyPanel";
import { ProtectionPanel } from "./ProtectionPanel";
import { SUITE_EDITOR_DRAFT, SUITE_VIEWS, useSuites, type SuiteRunHandoff, type SuitesPlace, type SuiteView } from "./Suites";
import { environmentPlace, useEnvironments } from "./Environments";
import { Reduction, type ReductionForm } from "./Reduction";
import { onRetentionResult, savedId } from "./drafting";
import { IndicatorsContext, useLifecycle, useWindowBusy } from "./lifecycle";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactElement } from "react";
import { useLayoutEffect } from "react";
import {
  buildReproducer,
  compareReproducers,
  cancel,
  compare,
  type CompareResult,
  createSampleWorkspace,
  createNamedProject,
  RequestScope,
  openNamedProject,
  listWholeCatalog,
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
  undoReproducer,
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
  readMessages,
  listViews,
  saveView,
  renameView,
  removeView,
  describeSearchSettings,
  saveSearchSettings,
  type GridQuery,
  type GridView,
  type MessagesResult,
  type MessageRow,
  type SearchSettings,
  type FieldState,
  type FindingStatus,
  type TestExpectation,
  normalizeCompare,
  type NormalizeResult,
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
  captureSample,
  recordView,
  type EditorDraft,
  search,
  selectWorkspace,
  shell,
  type CaseResult,
  type CommandId,
  type Indicator,
  type Match,
  type ProjectOverviewResult,
  type RegionId,
  type SearchResult,
  type Shell,
  type State,
  type ItemDraftResult,
  type ItemRef,
  type AttachmentsResult,
  type WorkspaceResult,
  messageFields,
  type RequestContext,
  onFileDrop,
} from "./bindings";
import { Comparison, readsComparison } from "./Comparison";
import { Review } from "./Review";
import { GuidedSample } from "./GuidedSample";
import { useTimeline } from "./Timeline";
import { MessageReader } from "./Inspector";
import { MessageList, NO_QUERY, sameQuery, SearchSettingsSheet, type FilterSeed } from "./Messages";
import { Reproducer, type ReproducerView } from "./Reproducer";
import { RevisionComparison, type ComparisonSeed } from "./RevisionComparison";
import { LibraryList, UseInTestSheet, importDraft, useCheckGroup, useLibraryItems, type LibraryKind } from "./Library";
import { useProfile } from "./ProfileLibrary";
import { useScenario } from "./ScenarioLibrary";
import { SampleFixture, ScenarioLibraryCheck, SyntheticFamilies } from "./SampleData";
import { SyntheticPackets } from "./SyntheticPackets";
import { useTests, type TestsPlace } from "./Tests";
import { TEST_EDITOR_DRAFT } from "./TestEditor";
import { useFindings, useSimilarFindings, type EvidenceRef } from "./Findings";
import { Report, Separator, Status } from "./shell";
import { CommandPalette, isMac, shortcut, type PaletteEntry } from "./CommandPalette";
import { ReturnAnchor, firstShownRow, type SortState } from "./DataTable";
import { ViewKey, forgetViewState } from "./viewstate";
import { sidebarOf, useRoutes, viewOf, type ReturnContext, type Route } from "./routes";
import { rootFontSize, useMeasured } from "./measure";
import {
  ICON_RAIL_REM,
  INSPECTOR_MAX_REM,
  INSPECTOR_MIN_REM,
  INSPECTOR_REM,
  LIST_MIN_REM,
  NARROW_WINDOW_REM,
  RAIL_BREAKPOINT_REM,
  SIDEBAR_REM,
} from "./geometry";
import { VocabularyContext } from "./vocabulary";
import { ImportFlow, importDrop } from "./Import";
import { useCapture } from "./Capture";
import { DeleteSourceSheet, StorageView } from "./Storage";
import { useFileReader } from "./RawInspection";
import { PerformanceCorpus } from "./PerformanceCorpus";
import type { CaptureObservationBinding } from "./bindings";
import { TaskPanel, TaskTabs } from "./TaskTabs";
import { IconButton } from "./IconButton";
import { DraftsToRestore, NewProjectSheet, ProjectList } from "./Projects";
import { CaseDetailsSheet, NoteSheet, ProjectSettingsSheet, RemoveCaseSheet, type ProjectSaveFailure } from "./CaseSheets";
import { AttachmentsList, FilesList, NotesList } from "./CaseContext";
import { CaseFacts, CaseFilterSheet, CaseList, CaseSearchSheet, caseChoices, NO_VIEW, type CaseAction, type CaseView as CaseListView } from "./Cases";
import {
  BackLink,
  Categories,
  EmptyState,
  FrameContext,
  GLOBAL_DESTINATIONS,
  Modal,
  Menu,
  NavItem,
  OperationIndicator,
  PROJECT_DESTINATIONS,
  Page,
  PaletteActionsContext,
  useOfferedActions,
  type MenuItem,
  type ObjectActions,
  ProjectSwitcher,
  folderName,
  type Destination,
} from "./layout";
import { DisplayTerm, PROVENANCES, SEARCH_FIELDS, term } from "./display";

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
  | "inspect"
  | "reproducer"
  | "comparison"
  | "revisions"
  | "practice"
  | "transformation"
  | "transform-plan"
  | "transform-open"
  | "reduction-preview"
  | "reduction"
  | "review"
  | "normalize"
  | "recent"
  | "sample";

/** The views inside a page. Each page keeps every view mounted, so an edit in
 * one survives looking at another. */
type CaseView = "messages" | "timeline" | "findings";
/** What a person does to a case, each on its own page with a way back. */
type CaseFlow = "replay" | "compare" | "reproduce" | "reduce";
const CASE_FLOW_TITLES: Record<CaseFlow, string> = {
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
  inspect: "Reading the message…",
  reproducer: "Updating the reproducer…",
  comparison: "Comparing…",
  revisions: "Comparing revisions…",
  practice: "Running the demo test…",
  transformation: "Previewing the transformation…",
  "transform-plan": "Saving the transformation plan…",
  "transform-open": "Opening the transformation plan…",
  "reduction-preview": "Previewing the reduction…",
  reduction: "Running the reduction…",
  review: "Reading the export review…",
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
  // Reading a case's messages is a background read of its own: it never holds
  // the window's one slot, so it never disables what a person is typing in, and
  // it never moves focus.
  const { running: readingMessages, run: readMessagesIn } = useLifecycle<"messages">({ background: true });
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
  const [inspectionResult, setInspectionResult] = useState<InspectionResult | null>(null);
  const [selectedOccurrence, setSelectedOccurrence] = useState<string | null>(null);
  // The open case's messages: the transient query and order they were read
  // under, and the rows read so far, window by window.
  // `more` is why a later page could not be read, while the rows before it stay.
  const [messages, setMessages] = useState<{ case: string; identity: string; result: MessagesResult; rows: MessageRow[]; more?: string } | null>(null);
  const [messageQuery, setMessageQuery] = useState<GridQuery>(NO_QUERY);
  const [messageSort, setMessageSort] = useState<SortState | null>(null);
  const [messageView, setMessageView] = useState("");
  const [views, setViews] = useState<GridView[]>([]);
  const [checkedMessages, setCheckedMessages] = useState<Set<string>>(new Set());
  // The messages a finding's evidence names, shown on their own until cleared.
  const [evidenceFocus, setEvidenceFocus] = useState<EvidenceRef[] | null>(null);
  // Where the evidence shown was opened from: this case's Findings, or a
  // comparison of similar findings, which Back returns to.
  const [evidenceFrom, setEvidenceFrom] = useState<"findings" | "similar">("findings");
  const [revealed, setRevealed] = useState(false);
  const [filterSeed, setFilterSeed] = useState<FilterSeed | null>(null);
  const [searchSettings, setSearchSettings] = useState<SearchSettings | null | undefined>(undefined);
  const [reproducerResult, setReproducerResult] = useState<ReproducerView | null>(null);
  const [runSpecPath, setRunSpecPath] = useState<string | undefined>(undefined);
  const [runEnvironment, setRunEnvironment] = useState<string | undefined>(undefined);
  // Counts each saved test's Run, which the run view preflights on arrival.
  const [runArrival, setRunArrival] = useState(0);
  // The saved suite version and environment Run handed the run view, which
  // preflights it and asks its own Send.
  const [suiteHandoff, setSuiteHandoff] = useState<{ workspace: string; handoff: SuiteRunHandoff } | null>(null);
  // What Schedule or Set up CI on a suite opens Runners with.
  const [runnerSeed, setRunnerSeed] = useState<{ tab: "schedules" | "ci"; suite: string; environment: string; count: number } | null>(null);
  const [comparisonResult, setComparisonResult] = useState<CompareResult | null>(null);
  const [revisionResult, setRevisionResult] = useState<ReproducerComparisonResult | null>(null);
  // The build the reproducer panel last handed to the revision comparison.
  const [comparisonSeed, setComparisonSeed] = useState<ComparisonSeed | null>(null);
  const [guideResult, setGuideResult] = useState<GuideResult | null>(null);
  const [practiceResult, setPracticeResult] = useState<PracticeResult | null>(null);
  const [sampleCapture, setSampleCapture] = useState<CaseResult | null>(null);
  const [transformResult, setTransformResult] = useState<TransformResult | null>(null);
  const [planResult, setPlanResult] = useState<TransformPlanResult | null>(null);
  const [reductionResult, setReductionResult] = useState<ReductionResult | null>(null);
  const [reviewResult, setReviewResult] = useState<ReviewResult | null>(null);
  const [normalizeResult, setNormalizeResult] = useState<NormalizeResult | null>(null);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const [watchedRun, setWatchedRun] = useState("");
  const [drafts, setDrafts] = useState<EditorDraft[] | null>(null);
  const [capturing, setCapturing] = useState(false);
  const [importing, setImporting] = useState(false);
  // Each new count opens Capture's setup sheet once.
  const [captureSetup, setCaptureSetup] = useState(0);
  // The retained capture Add observation starts from, when a capture started it.
  const [captureBinding, setCaptureBinding] = useState<CaptureObservationBinding | null>(null);

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
  // The actions the shown pages' selected objects offer the palette.
  const [objectActions, setObjectActions] = useState<ReadonlyMap<symbol, ObjectActions>>(new Map());
  const registerActions = useCallback((owner: symbol, actions: ObjectActions | null) => {
    setObjectActions((held) => {
      if (!actions && !held.has(owner)) return held;
      const next = new Map(held);
      if (actions) next.set(owner, actions);
      else next.delete(owner);
      return next;
    });
  }, []);
  const preferences = usePreferences();
  const [startUpdate, setStartUpdate] = useState(false);
  // Add observation opened from Security's Add connection returns there.
  const [observingFromSecurity, setObservingFromSecurity] = useState(false);
  // The connection Security selects when a setup started there comes back
  // saved, and the setups Security starts on the Team, Runners and License
  // pages: each new count starts that page's setup once.
  const [securityReturn, setSecurityReturn] = useState<string | undefined>(undefined);
  const [setupFromSecurity, setSetupFromSecurity] = useState<null | "team" | "operator" | "portal" | "runner">(null);
  const [runnerPath, setRunnerPath] = useState<string | undefined>(undefined);
  const [setupRequests, setSetupRequests] = useState({ team: 0, operator: 0, portal: 0, runner: 0 });
  const [packagesOpen, setPackagesOpen] = useState(false);
  // The inspector width each project was last given, in rem. The shown width
  // is clamped to the room there is now without overwriting the wider choice.
  const [inspectorWidths, setInspectorWidths] = useState<Record<string, number>>({});

  // Where the window is: one route, with the way back kept per destination.
  // Only the shown destination is mounted; what a page holds unsaved is kept
  // in the view state for this project.
  const { route, dispatch: routeTo } = useRoutes({ destination: "home" });
  const place = route.destination;
  const destination = sidebarOf(place);
  const currentRoute = useRef<Route>(route);
  currentRoute.current = route;
  const caseView = viewOf(route, "cases", CASE_VIEWS);
  const testsView = viewOf(route, "tests", TESTS_VIEWS);
  // Library opens on Profiles, then on the category last chosen in this project.
  const [libraryViews, setLibraryViews] = useState<Record<string, LibraryView>>({});
  const routedLibraryView = route.destination === "library" && LIBRARY_VIEWS.some((view) => view.key === route.view) ? (route.view as LibraryView) : undefined;
  const libraryView: LibraryView = routedLibraryView ?? libraryViews[route.projectId ?? ""] ?? "profiles";
  useEffect(() => {
    if (routedLibraryView) setLibraryViews((held) => (held[route.projectId ?? ""] === routedLibraryView ? held : { ...held, [route.projectId ?? ""]: routedLibraryView }));
  }, [routedLibraryView, route.projectId]);
  const settingsView = viewOf(route, "settings", SETTINGS_VIEWS);
  const setView = useCallback((view: string) => routeTo({ type: "view", view }), [routeTo]);
  // What the place being left had, for Back to restore: the selection named,
  // and how far its page had been scrolled.
  const leaving = useCallback((selection?: string): ReturnContext => {
    const body = document.querySelector<HTMLElement>(`.page[data-page="${currentRoute.current.destination}"] .page-body`);
    const list = body?.querySelector<HTMLElement>(".table-view");
    const anchor = list ? firstShownRow(list) : undefined;
    return { ...(selection !== undefined ? { selection } : {}), ...(body ? { scrollTop: body.scrollTop } : {}), ...(anchor !== undefined ? { anchor } : {}) };
  }, []);
  const returnAnchor = useMemo(() => ({ anchor: route.returnContext?.anchor, at: route.returnContext ? route : undefined }), [route]);
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
  // The project or case chosen for Delete from this computer, with the
  // project its review is read under.
  const [deleting, setDeleting] = useState<{ item: CatalogItem; context: RequestContext } | null>(null);
  // What the notes, attachments and files pages show, and the note being
  // written.
  const [notes, setNotes] = useState<NoteItem[]>([]);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [files, setFiles] = useState<ProjectFile[]>([]);
  const [noteEditing, setNoteEditing] = useState<{ note: NoteItem | null } | null>(null);
  // The check group a test's editor adds to its draft when it opens.
  const [pendingCheckGroup, setPendingCheckGroup] = useState<ItemRef | null>(null);

  // Why a row's last Locate or Remove from recents was refused, by identity,
  // and why an attachment was not added or removed.
  const [projectNotices, setProjectNotices] = useState<Record<string, string>>({});
  const [caseNotices, setCaseNotices] = useState<Record<string, string>>({});
  const [attachmentNotice, setAttachmentNotice] = useState<string | null>(null);
  // The project's cases are being read after it was opened.
  const [casesLoading, setCasesLoading] = useState(false);
  // The case whose recorded details are shown.
  const [caseFacts, setCaseFacts] = useState<CatalogItem | null>(null);
  // A retained draft being resumed: once its project is open, the sheet it
  // belongs to opens with it.
  const [resuming, setResuming] = useState<EditorDraft | null>(null);
  const [restored, setRestored] = useState<EditorDraft | null>(null);
  // Why a draft chosen to resume found nothing to reopen.
  const [resumeNotice, setResumeNotice] = useState<string | null>(null);
  // A retained test editor draft the Tests editor reopens.
  const [restoringTest, setRestoringTest] = useState<EditorDraft | null>(null);
  const [restoringSuite, setRestoringSuite] = useState<EditorDraft | null>(null);
  const [searching, setSearching] = useState(false);
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
    const listed = await listWholeCatalog({ context: { project: "", generation: 0 }, kind: "project", filter: {} });
    if (listed.page) setProjects(listed.page.items);
  }, []);

  // The guided sample is read back out of the open folder rather than
  // remembered, so it is re-read whenever that folder or what it holds may have
  // changed. Nothing about where somebody is in it lives in this window.
  const refreshGuide = useCallback(async (folder: string | null) => {
    setGuideResult(folder ? await readGuide(folder) : null);
  }, []);

  // Projects is read again each time it is shown, so a project moved or
  // removed meanwhile is listed as it is now.
  const onProjects = place === "home";
  useEffect(() => {
    if (onProjects) void refreshRecent();
  }, [onProjects, refreshRecent]);

  // The saved filters and the selected one live in the facade, not here, so the
  // view a person set up survives navigating to another case and reopening the
  // window. They are read once and after every change.
  const refreshFilters = useCallback(async () => {
    await readFilters();
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

  // Whether an operation holds the window's one slot: this window's own, or a
  // screen's whose call holds the facade, such as the raw inspection and the
  // performance corpus.
  const busy = useWindowBusy();
  const fileReader = useFileReader({ busy, request: rawRequest });
  const root = workspace?.workspace?.root ?? null;
  // Another project, or none, starts with nothing any page held for the last.
  const viewRoot = useRef(root);
  if (viewRoot.current !== root) {
    viewRoot.current = root;
    forgetViewState();
  }
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
    setMessages(null);
    setMessageQuery(NO_QUERY);
    setMessageSort(null);
    setMessageView("");
    setCheckedMessages(new Set());
    setRevealed(false);
    setFilterSeed(null);
    setInspectionResult(null);
    setReproducerResult(null);
    setComparisonResult(null);
    setRevisionResult(null);
    setComparisonSeed(null);
    setTransformResult(null);
    setPlanResult(null);
    setReductionResult(null);
    setReviewResult(null);
    setNormalizeResult(null);
    setEvidenceFocus(null);
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
  // The open case's messages under the applied query, one window at a time.
  // A new query or order reads from the first window; scrolling to the end
  // asks for the next and appends it. Nothing is saved by reading: the facade
  // reuses the case's own index or reads the case directly. Only the answer to
  // the latest request commits.
  const loadMessages = useCallback(
    async (folder: string, open: { case: string; identity: string }, query: GridQuery, sort: SortState | null, offset: number, occurrences?: string[]) => {
      await readMessagesIn("messages", async (current) => {
        if (offset === 0) {
          setInspectionResult(null);
          setSelectedOccurrence(null);
          setCheckedMessages(new Set());
        }
        const result = await readMessages({
          workspace: folder,
          case: open.case,
          identity: open.identity,
          query,
          sort: sort?.column === "time" ? (sort.direction === "ascending" ? "time-ascending" : "time-descending") : "",
          offset,
          limit: 0,
          ...(occurrences ? { occurrences } : {}),
        });
        if (!current()) return;
        // A later page that is refused leaves the rows already read on
        // screen, with the reason and a way to ask again.
        if (offset > 0 && result.state !== "completed") {
          setMessages((held) => (held ? { ...held, more: result.reason ?? "The rest of the messages could not be read." } : held));
          return;
        }
        setMessages((held) => ({
          ...open,
          result,
          rows: offset === 0 || !held ? result.rows : [...held.rows, ...result.rows],
        }));
      });
    },
    [readMessagesIn],
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
          autoIndex = result.case.identity;
        } else {
          setCaseNotice(result);
        }
      });
      if (autoIndex && !options?.skipAutoGrid) {
        void loadMessages(folder, { case: name, identity: autoIndex }, NO_QUERY, null, 0);
        void listViews(folder).then((answer) => setViews(answer.views));
      }
      focusRegion("evidence");
      return outcome;
    },
    [clearCase, focusRegion, loadMessages, run],
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
      reveal: boolean = revealed,
      rawOffset = -1,
    ): Promise<InspectionResult | null> => {
      const grid = messages;
      const open =
        targetCase ??
        (grid
          ? { case: grid.case, identity: grid.identity }
          : evidence?.case
            ? { case: evidence.case.name, identity: evidence.case.identity }
            : null);
      if (!root || !open) return null;
      let answer: InspectionResult | null = null;
      await run("inspect", async (current) => {
        const moving = occurrence !== selectedOccurrence;
        setSelectedOccurrence(occurrence);
        if (moving) setInspectionResult(null);
        const result = await inspectOccurrence({
          workspace: root,
          case: open.case,
          identity: open.identity,
          occurrence,
          path,
          node_offset: nodeOffset,
          byte_offset: byteOffset,
          raw_offset: rawOffset,
          reveal,
        });
        answer = result;
        // A field that is not in the message leaves the message as it was.
        if (current() && (result.state === "completed" || moving || path === "")) {
          setInspectionResult(result);
        }
      });
      return answer;
    },
    [evidence, messages, revealed, root, run, selectedOccurrence],
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

  // Run on a suite hands one exact saved version and the environment chosen
  // to the run view, which preflights it at once and asks its own Send.
  const handoff = useCallback(
    (selection: SuiteRunHandoff) => {
      setRunSpecPath(undefined);
      setRunEnvironment(selection.environment);
      if (root) setSuiteHandoff({ workspace: root, handoff: selection });
      setRunArrival((count) => count + 1);
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
      const answer = await createNamedProject({ name, ...(newProjectParent ? { location: newProjectParent } : {}) });
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
    [newProjectParent, openFolder],
  );

  // Continuing a retained draft opens its project and then the editor it
  // belongs to, where the draft comes back with its unsaved marker. Nothing
  // it describes is sent or applied.
  const resumeDraft = useCallback(
    async (draft: EditorDraft) => {
      if (draft.workspace !== root && !(await openFolder(() => openWorkspace(draft.workspace)))) return;
      if (SHEET_DRAFTS.has(draft.kind)) {
        setResumeNotice(null);
        setResuming(draft);
        return;
      }
      if (draft.kind === "test-draft" && draft.content_schema === TEST_EDITOR_DRAFT) {
        setResumeNotice(null);
        setRestoringTest(draft);
        return;
      }
      if (draft.kind === "suite-editor" && draft.content_schema === SUITE_EDITOR_DRAFT) {
        setResumeNotice(null);
        setRestoringSuite(draft);
        return;
      }
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
        setCaptureBinding(null);
        routeTo({ type: "go", to: { destination: "environments", view: "add-observation" } });
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
    const listed = await listWholeCatalog({ context, kind: "case", filter: {} });
    if (!projectScope.current.current(listed)) return;
    setCases(listed.page?.items ?? []);
    listedCases.current = listed.page?.items ?? [];
    setCasesLoading(false);
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
      if (action === "details") {
        setCaseFacts(item);
        return;
      }
      if (action === "delete") {
        setDeleting({ item, context: projectContext() });
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
    setAttachmentNotice(null);
    const answer = await listAttachments({ context: projectContext(), ref: contextCase.ref });
    if (projectScope.current.current(answer)) setAttachments(answer.attachments);
  }, [contextCase, projectContext]);

  // An add or remove answers the case's attachments as they are now, with
  // the reason when it was refused.
  const attached = useCallback((answer: AttachmentsResult) => {
    if (answer.state === "cancelled") return;
    setAttachments(answer.attachments);
    setAttachmentNotice(answer.state === "completed" || answer.state === "empty" ? null : (answer.reason ?? null));
  }, []);

  useEffect(() => {
    if (place === "notes" || place === "case-notes") void refreshNotes();
    if (place === "case-attachments") void refreshAttachments();
    if (place === "project-files" && root) {
      void projectFiles({ context: projectContext(), ref: currentProject?.ref ?? { kind: "project", id: "" } }).then((answer) => {
        if (projectScope.current.current(answer)) setFiles(answer.files);
      });
    }
  }, [currentProject, place, projectContext, refreshAttachments, refreshNotes, root]);

  // Another project opened: nothing of the one before is shown while its own
  // cases are read.
  useEffect(() => {
    setCaseListView(NO_VIEW);
    setCaseListSort(null);
    setCaseTask(null);
    setCases([]);
    listedCases.current = [];
    setSelectedCase(null);
    setNotes([]);
    setAttachments([]);
    setFiles([]);
    setCaseNotices({});
    setAttachmentNotice(null);
    setResumeNotice(null);
    setCasesLoading(true);
    void refreshCases();
  }, [refreshCases]);

  // A retained sheet draft reopens its sheet once its project is open and its
  // object is listed. One whose object is gone stays among the drafts.
  useEffect(() => {
    if (!resuming || root !== resuming.workspace || casesLoading) return;
    const ref = resuming.item?.ref;
    setResuming(null);
    if (!currentProject || currentProject.ref.id !== resuming.item?.project_id) {
      setResumeNotice("This folder no longer holds the project this draft was written in.");
      return;
    }
    if (resuming.kind === "project") {
      setRestored(resuming);
      setEditingProject(true);
      return;
    }
    const subject = ref?.kind === "case" ? cases.find((item) => item.ref.id === ref.id) : undefined;
    if (resuming.kind === "case") {
      if (!subject) {
        setResumeNotice("The case this draft edits is no longer in the project.");
        return;
      }
      setRestored(resuming);
      setCaseTask({ item: subject, action: "edit" });
      return;
    }
    routeTo({ type: "go", to: subject ? { destination: "case-notes", objectId: subject.ref.id } : { destination: "notes" } });
    setRestored(resuming);
    setNoteEditing({ note: null });
  }, [cases, casesLoading, currentProject, resuming, root, routeTo]);

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

  // The case an import or a finished capture published opens on its
  // Messages, once the project lists it.
  const openImportedCase = useCallback(
    async (ref: ItemRef) => {
      await leaveImport();
      await refreshCases();
      const entry = listedCases.current.find((item) => item.ref.id === ref.id)?.summary.case?.entry;
      if (root && entry) await verifyCase(root, entry);
    },
    [leaveImport, refreshCases, root, verifyCase],
  );

  // Files dropped on the window go to the Import flow while it is open. One
  // listener is registered for the window's life, which also stops the
  // webview opening a dropped file itself.
  useEffect(() => onFileDrop((paths) => importDrop.deliver?.(paths)), []);

  // Capture and Import read under their own scopes, so their reads never make
  // the Cases list's answer look stale.
  const captureScope = useRef(new RequestScope());
  const captureContext = useCallback(() => captureScope.current.enter(root ?? ""), [root]);
  const importScope = useRef(new RequestScope());
  const importContext = useCallback(() => importScope.current.enter(root ?? ""), [root]);
  const capture = useCapture({
    root,
    context: captureContext,
    busy,
    setupRequest: captureSetup,
    onOpenCase: (ref) => {
      setCapturing(false);
      void openImportedCase(ref);
    },
  });
  const startCaptureSetup = () => {
    setCapturing(true);
    setCaptureSetup((count) => count + 1);
  };

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
      const open = messages;
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
    [dropReproducerDraft, messages, keepReproducerDraft, reproducerResult, root, run],
  );

  // Create variant from checked messages: a new plan that selects exactly
  // them, opened in the reproducer. Nothing is kept as a draft until the
  // person edits it there.
  const seedVariant = useCallback(
    async (occurrences: string[]) => {
      const open = messages;
      if (!root || !open || occurrences.length === 0) return;
      let plan: ReproducerPlan = { schema: "", case: "", steps: [] };
      let last: ReproducerResult | null = null;
      await run("reproducer", async () => {
        for (const occurrence of occurrences) {
          last = await editReproducer({ workspace: root, case: open.case, identity: open.identity, plan, step: { operator: "select-occurrence/v1", occurrence } });
          if (!last.reproducer) break;
          plan = last.reproducer.plan;
        }
      });
      if (!last) return;
      setReproducerResult(last);
      setCaseFlow("reproduce");
    },
    [messages, root, run],
  );

  // The reproducer plan comes back the same way, and under the same rule.
  useEffect(() => {
    const grid = messages;
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
  }, [drafts, messages, reproducerResult, root]);

  // Drops one retained editor draft and takes it out of the local list at the
  // same moment, so a panel cannot offer the same draft back again while the
  // facade's answer is still in flight.
  const dropDraft = useCallback((id: string) => {
    setDrafts((current) => current?.filter((draft) => draft.id !== id) ?? null);
    void discardEditorDraft(id);
  }, []);

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

  // Environments reads under its own request scope, so its reads never make
  // another list's answer look stale.
  const environmentScope = useRef(new RequestScope());
  const environmentContext = useCallback(() => environmentScope.current.enter(root ?? ""), [root]);
  // Tests reads under its own request scope. A saved test, a new test and an
  // edit are each their own place, with Back to where they were opened.
  const testsPlace: TestsPlace =
    place === "new-test"
      ? { kind: "new" }
      : place === "edit-test" && route.objectId
        ? { kind: "edit", id: route.objectId }
        : place === "tests" && route.objectId
          ? { kind: "test", id: route.objectId, view: route.view === "checks" || route.view === "history" ? route.view : "setup" }
          : { kind: "list" };
  const tests = useTests({
    root,
    addCheckGroup: pendingCheckGroup,
    onCheckGroupAdded: () => setPendingCheckGroup(null),
    restoreDraft: restoringTest,
    inspectedField: inspectionResult?.inspection?.selected.path,
    onRestored: (reason) => {
      setRestoringTest(null);
      if (reason) setResumeNotice(reason);
    },
    shown: place === "tests" || place === "new-test" || place === "edit-test",
    place: testsPlace,
    go: (to) => {
      switch (to.kind) {
        case "list":
          open({ destination: "tests" });
          return;
        case "new":
          open({ destination: "new-test" });
          return;
        case "edit":
          open({ destination: "edit-test", objectId: to.id });
          return;
        case "test":
          if (place === "new-test") routeTo({ type: "replace", to: { destination: "tests", objectId: to.id, view: to.view } });
          else if (place === "tests" && route.objectId === to.id) routeTo({ type: "view", view: to.view });
          else open({ destination: "tests", objectId: to.id, view: to.view });
      }
    },
    back,
    busy,
    onRun: (entry) => {
      setRunSpecPath(entry);
      setRunEnvironment(undefined);
      setSuiteHandoff(null);
      setRunArrival((count) => count + 1);
      open({ destination: "run-test" });
    },
    onLibrary: () => open({ destination: "library" }),
  });

  // Suites: the Suites tab of Tests, a saved suite, its editor and the review
  // of two versions, each its own place with Back to where it was opened.
  const suiteView = (view: string | undefined): SuiteView => (SUITE_VIEWS.some((entry) => entry.key === view) ? (view as SuiteView) : "tests");
  const suitesPlace: SuitesPlace =
    place === "suite" && route.objectId
      ? { kind: "suite", id: route.objectId, view: suiteView(route.view) }
      : place === "edit-suite"
        ? { kind: "edit", id: route.objectId ?? "", view: suiteView(route.view) }
        : place === "suite-review" && route.objectId
          ? { kind: "review", id: route.objectId, from: (route.view ?? "").split("..")[0] ?? "", to: (route.view ?? "").split("..")[1] ?? "" }
          : { kind: "list" };
  const suites = useSuites({
    root,
    shown: (place === "tests" && testsView === "suites") || place === "suite" || place === "edit-suite" || place === "suite-review",
    place: suitesPlace,
    go: (to) => {
      switch (to.kind) {
        case "list":
          open({ destination: "tests", view: "suites" });
          return;
        case "edit":
          if (place === "edit-suite" && (route.objectId ?? "") === to.id) routeTo({ type: "view", view: to.view });
          else open({ destination: "edit-suite", ...(to.id ? { objectId: to.id } : {}), view: to.view });
          return;
        case "review":
          open({ destination: "suite-review", objectId: to.id, view: `${to.from}..${to.to}` });
          return;
        case "suite":
          if (place === "edit-suite") routeTo({ type: "replace", to: { destination: "suite", objectId: to.id, view: to.view } });
          else if (place === "suite" && route.objectId === to.id) routeTo({ type: "view", view: to.view });
          else open({ destination: "suite", objectId: to.id, view: to.view });
      }
    },
    back,
    busy,
    onRun: handoff,
    onLibrary: () => open({ destination: "library" }),
    onSchedule: (entry) => {
      setRunnerSeed((held) => ({ tab: "schedules", suite: entry, environment: "", count: (held?.count ?? 0) + 1 }));
      open({ destination: "settings", view: "runners" });
    },
    onSetUpCI: (entry, environment) => {
      setRunnerSeed((held) => ({ tab: "ci", suite: entry, environment, count: (held?.count ?? 0) + 1 }));
      open({ destination: "settings", view: "runners" });
    },
    restoreDraft: restoringSuite,
    onRestored: (reason) => {
      setRestoringSuite(null);
      if (reason) setResumeNotice(reason);
    },
  });

  /** Create test: the case open now and its chosen messages (all of them
   * when none is chosen) go to the one test editor. */
  // The object the open case is: a project case, or a variant of one.
  const openCaseObject = async (): Promise<{ ref: ItemRef; variant: boolean } | null> => {
    if (!verified) return null;
    // A variant's file can also be listed as a case; it is still the variant.
    const variants = await listWholeCatalog({ context: projectContext(), kind: "variant", filter: {} });
    const variant = variants.page?.items.find((item) => item.summary.variant?.entry === verified.name)?.ref;
    if (variant) return { ref: variant, variant: true };
    const listed = listedCases.current.find((item) => item.summary.case?.entry === verified.name)?.ref;
    return listed ? { ref: listed, variant: false } : null;
  };
  const createTest = async () => {
    const origin = await openCaseObject();
    if (!origin) {
      void tests.startNew();
      return;
    }
    // With nothing chosen, the facade selects every message of the case a test can send.
    const chosen = messages ? messages.rows.filter((row) => checkedMessages.has(row.id) && row.kind === "message").map((row) => row.id) : [];
    void tests.startNew({ case: origin.ref, messages: chosen, ...(origin.variant ? { source: { kind: "variant", variant: origin.ref } } : {}) });
  };

  // A confirmed finding opens the same editor with the review's messages and
  // its expectations as proposals, each undecided until the person decides.
  const promoteFinding = async (status: FindingStatus, reviewEntry: string, reportSHA256: string, title?: string) => {
    const promotion = status.promotion;
    const origin = await openCaseObject();
    if (!promotion || !origin) return;
    void tests.startNew({
      case: origin.ref,
      messages: promotion.messages,
      ...(title ? { title } : {}),
      source: { kind: "finding", finding: status.finding, report_sha256: reportSHA256, ...(reviewEntry ? { review: reviewEntry } : {}) },
      proposals: promotion.expectations.map((check: TestExpectation, index: number) => ({ id: `finding-${index + 1}`, source: "finding", check })),
    });
  };

  // Findings reads under its own request scope, only while it is shown.
  const findingsCase = verified ? (listedCases.current.find((item) => item.summary.case?.entry === verified.name)?.ref ?? null) : null;
  const findings = useFindings({
    root,
    caseRef: findingsCase,
    caseName: verified ? (cases.find((item) => item.summary.case?.entry === verified.name)?.name ?? verified.name) : "",
    caseEntry: verified?.name ?? "",
    identity: verified?.identity ?? "",
    shown: place === "cases" && caseView === "findings" && verified !== null,
    busy,
    onViewMessages: (evidence) => {
      if (!root || !verified) return;
      const shown = { case: verified.name, identity: verified.identity };
      setEvidenceFocus(evidence);
      setEvidenceFrom("findings");
      setView("messages");
      // The first piece of evidence opens selected, at its field.
      void loadMessages(root, shown, messageQuery, messageSort, 0, [...new Set(evidence.map((entry) => entry.occurrence))]).then(() => {
        const first = evidence[0];
        if (first) void inspect(first.occurrence, first.field, 0, -1, shown);
      });
    },
    onSimilar: () => open({ destination: "similar-findings" }),
    onOpenComparison: (ref) => open({ destination: "similar-findings", objectId: ref.id }),
    onCreateTest: (status, review, reportSHA256, title) => void promoteFinding(status, review, reportSHA256, title),
  });
  const similar = useSimilarFindings({
    root: place === "similar-findings" ? root : null,
    caseRef: findingsCase,
    saved: place === "similar-findings" ? route.objectId : undefined,
    busy,
    // A member case opens on exactly its evidence, and Back returns here.
    onViewEvidence: (ref, occurrences) => {
      const entry = listedCases.current.find((item) => item.ref.id === ref.id)?.summary.case?.entry;
      if (!root || !entry) return;
      void verifyCase(root, entry, { skipAutoGrid: true }).then((opened) => {
        if (!opened?.case) return;
        setEvidenceFocus(occurrences.map((occurrence) => ({ occurrence, field: "" })));
        setEvidenceFrom("similar");
        void loadMessages(root, { case: entry, identity: opened.case.identity }, NO_QUERY, null, 0, occurrences);
      });
    },
  });

  // Library: each tab's list, and the one object open in it.
  const libraryScope = useRef(new RequestScope());
  const libraryContext = useCallback(() => libraryScope.current.enter(root ?? ""), [root]);
  const LIBRARY_KINDS: Record<LibraryView, LibraryKind> = { checks: "check-group", profiles: "profile", scenarios: "scenario" };
  const libraryKind = LIBRARY_KINDS[libraryView];
  const libraryObject = place === "library" ? route.objectId : undefined;
  const onLibraryList = place === "library" && libraryObject === undefined;
  const [libraryImport, setLibraryImport] = useState<ItemDraftResult | null>(null);
  const [libraryNotice, setLibraryNotice] = useState<string | null>(null);
  const libraryLists: Record<LibraryView, ReturnType<typeof useLibraryItems>> = {
    checks: useLibraryItems("check-group", libraryContext, onLibraryList && libraryView === "checks"),
    profiles: useLibraryItems("profile", libraryContext, onLibraryList && libraryView === "profiles"),
    scenarios: useLibraryItems("scenario", libraryContext, onLibraryList && libraryView === "scenarios"),
  };
  const openLibraryObject = useCallback(
    (objectId: string) => routeTo({ type: "go", to: { destination: "library", view: libraryView, objectId }, leaving: leaving(objectId) }),
    [leaving, libraryView, routeTo],
  );
  // A save names what it saved; a new object then opens as saved, and a new
  // one left unsaved returns to its list.
  const librarySaved = useCallback(
    (saved: ItemRef) => {
      setLibraryImport(null);
      if (saved.id === "") routeTo({ type: "back" });
      else routeTo({ type: "replace", to: { destination: "library", view: libraryView, objectId: saved.id } });
    },
    [libraryView, routeTo],
  );
  const libraryRef = (kind: LibraryKind): ItemRef | null =>
    libraryKind === kind && libraryObject && libraryObject !== "new" && libraryObject !== "import" ? { kind, id: libraryObject } : null;
  const libraryImported = libraryObject === "import" ? libraryImport : null;
  const checkGroupPage = useCheckGroup({
    context: libraryContext,
    ref: libraryRef("check-group"),
    imported: libraryKind === "check-group" ? libraryImported : null,
    shown: place === "library" && libraryView === "checks" && libraryObject !== undefined,
    busy,
    onSaved: librarySaved,
    onUseInTest: (ref, name) => setUsingInTest({ ref, name }),
  });
  const profilePage = useProfile({
    onImport: () => void importLibrary(),
    context: libraryContext,
    ref: libraryRef("profile"),
    imported: libraryKind === "profile" ? libraryImported : null,
    shown: place === "library" && libraryView === "profiles" && libraryObject !== undefined,
    busy,
    onSaved: librarySaved,
  });
  const scenarioPage = useScenario({
    context: libraryContext,
    ref: libraryRef("scenario"),
    imported: libraryKind === "scenario" ? libraryImported : null,
    shown: place === "library" && libraryView === "scenarios" && libraryObject !== undefined,
    busy,
    onSaved: librarySaved,
    onOpenCase: (_ref, entry) => {
      if (root) void verifyCase(root, entry);
    },
  });
  const libraryPage = libraryView === "checks" ? checkGroupPage : libraryView === "profiles" ? profilePage : scenarioPage;
  const importLibrary = async () => {
    setLibraryNotice(null);
    const answer = await importDraft(libraryKind, libraryContext());
    if (answer.state === "cancelled") return;
    if (answer.state !== "completed" || !("draft" in answer) || !answer.draft) {
      setLibraryNotice(("reason" in answer ? answer.reason : undefined) ?? "The file was not imported.");
      return;
    }
    setLibraryImport(answer);
    openLibraryObject("import");
  };
  // Use in test: the chosen check group, until a test is picked for it.
  const [usingInTest, setUsingInTest] = useState<{ ref: ItemRef; name: string } | null>(null);

  // Timeline reads under its own request scope, only while it is shown.
  const timelineCase = verified ? (listedCases.current.find((item) => item.summary.case?.entry === verified.name)?.ref ?? null) : null;
  const timeline = useTimeline({
    root,
    caseRef: timelineCase,
    caseEntry: verified?.name ?? "",
    identity: verified?.identity ?? "",
    shown: place === "cases" && caseView === "timeline" && verified !== null,
    busy,
    onInspect: (occurrence) => void inspect(occurrence, "", 0, -1),
    onImport: () => setImporting(true),
  });

  const environments = useEnvironments({
    root,
    context: environmentContext,
    place: environmentPlace(place === "environments" ? route.objectId : undefined, route.view),
    go: (objectId, view) => open({ destination: "environments", objectId, ...(view ? { view } : {}) }),
    back,
    busy,
    onAdded: (ref) => returnToSecurity(ref),
    capture: captureBinding,
    onObservationClosed: () => {
      setCaptureBinding(null);
      if (observingFromSecurity) {
        setObservingFromSecurity(false);
        returnToSecurity();
      } else back();
    },
    onObservationSaved: (id) => {
      setCaptureBinding(null);
      if (observingFromSecurity) {
        setObservingFromSecurity(false);
        returnToSecurity(`observation:${id}`);
      } else open({ destination: "environments", objectId: `observation:${id}` });
    },
  });

  const encryption = useEncryption({ root: place === "encryption" ? root : null, busy, onChanged: () => void refreshListing() });

  // The shown test's or environment's own actions, for the palette.
  useOfferedActions(registerActions, place === "tests" && "palette" in tests ? tests.palette : null);
  useOfferedActions(registerActions, place === "environments" && "palette" in environments ? environments.palette : null);

  // Storage reads under its own request scope, so its reads never make
  // another list's answer look stale.
  const storageScope = useRef(new RequestScope());
  const storageContext = useCallback(() => storageScope.current.enter(root ?? ""), [root]);

  const openSettings = useCallback((view: SettingsView) => open({ destination: "settings", view }), [open]);

  const openStorage = useCallback(() => openSettings("storage"), [openSettings]);

  /** Back to Security from a setup it started: with the saved connection
   * selected, or with nothing selected when the setup was closed unsaved. */
  const returnToSecurity = (ref?: string) => {
    setSecurityReturn(ref);
    open({ destination: "settings", view: "security" });
  };
  const requestSetup = (which: keyof typeof setupRequests) => setSetupRequests((held) => ({ ...held, [which]: held[which] + 1 }));
  const setupHandled = (which: keyof typeof setupRequests) => () => setSetupRequests((held) => ({ ...held, [which]: 0 }));
  // A setup Security started ends with its first choice: a saved one returns
  // to Security, a cancelled one stays on its page and forgets Security.
  const configured = (which: "team" | "operator" | "portal" | "runner", ref: string) => (chosen: boolean) => {
    if (setupFromSecurity !== which) return;
    setSetupFromSecurity(null);
    if (chosen) returnToSecurity(ref);
  };
  /** Security's Add connection and Edit: the owner of that kind's setup. */
  const openConnection = (route: ConnectionRoute) => {
    setSecurityReturn(undefined);
    switch (route.kind) {
      case "environment":
        open({ destination: "environments", ...(route.id ? { objectId: route.id } : {}), view: route.id ? "edit" : "add" });
        return;
      case "observation":
        if (route.id) {
          open({ destination: "environments", objectId: `observation:${route.id}`, view: "edit" });
        } else {
          setCaptureBinding(null);
          setObservingFromSecurity(true);
          open({ destination: "environments", view: "add-observation" });
        }
        return;
      case "team":
        setSetupFromSecurity(route.operator ? "operator" : "team");
        requestSetup(route.operator ? "operator" : "team");
        openSettings("team");
        return;
      case "runner":
        setRunnerPath(route.path);
        setSetupFromSecurity("runner");
        requestSetup("runner");
        openSettings("runners");
        return;
      case "portal":
        setSetupFromSecurity("portal");
        requestSetup("portal");
        openSettings("license");
        return;
      case "license":
        openSettings("license");
        return;
    }
  };

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
    "create-test": () => void createTest(),
    "create-report": () => go("reports"),
    "manage-profiles": () => open({ destination: "library", view: "profiles" }),
    "maintain-workspace": () => openStorage(),
    "check-staged-upgrade": () => openStorage(),
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
    "larger-text": () => {
      const scales = described?.text_scales ?? [100];
      const next = scales.find((percent) => percent > preferences.saved.text_scale);
      if (next !== undefined) void preferences.save({ ...preferences.saved, text_scale: next });
    },
    "smaller-text": () => {
      const scales = described?.text_scales ?? [100];
      const next = [...scales].reverse().find((percent) => percent < preferences.saved.text_scale);
      if (next !== undefined) void preferences.save({ ...preferences.saved, text_scale: next });
    },
    "switch-theme": () => {
      const themes = described?.themes ?? ["system"];
      const next = themes[(themes.indexOf(preferences.saved.theme) + 1) % themes.length];
      void preferences.save({ ...preferences.saved, theme: next ?? "system" });
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
        // ⌘ on macOS and Ctrl elsewhere, never the other: Ctrl+K on a Mac
        // and the Windows key elsewhere belong to the system.
        const mac = isMac();
        if (!(mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey)) {
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
  const subpage = capturing && root ? "capture" : null;
  // The open case's own menu, which the palette also lists while the case is
  // on screen.
  const caseMenu: MenuItem[] = [
    { label: "Create test", onSelect: () => void createTest() },
    { label: "Replay", onSelect: () => setCaseFlow("replay") },
    { label: "Compare with another case", onSelect: () => setCaseFlow("compare") },
    { label: "Build a reproducer", onSelect: () => setCaseFlow("reproduce") },
    { label: "Reduce", onSelect: () => setCaseFlow("reduce") },
    { label: "File details", onSelect: () => setFileDetails(true) },
    { label: "Close case", onSelect: backToProject },
  ];
  useOfferedActions(registerActions, place === "cases" && verified !== null && subpage === null && caseFlow === null ? { object: caseTitle, items: caseMenu } : null);
  const inspecting = selectedOccurrence !== null || inspectionResult !== null || running === "inspect";
  // The rows chosen for Create test and Send selected: only message rows, so an
  // ACK or unparsed row is never counted as outbound; none chosen is all rows.
  const chosenRows = messages ? (checkedMessages.size > 0 ? messages.rows.filter((row) => checkedMessages.has(row.id) && row.kind === "message") : messages.rows) : [];
  const shownCase = verified ? { case: verified.name, identity: verified.identity } : { case: "", identity: "" };
  const selectedRow = messages?.rows.find((row) => row.id === selectedOccurrence) ?? null;
  const fileSelection = place === "inspect-file" && fileReader.details !== null;
  const findingShown = place === "cases" && verified !== null && subpage === null && caseView === "findings" && findings.selected !== null;
  const linkShown = place === "cases" && verified !== null && subpage === null && caseView === "timeline" && timeline.selectedLink !== null;
  const detailsShown =
    (place === "cases" && verified !== null && subpage === null && caseView !== "findings" && inspecting && !linkShown) || findingShown || linkShown || fileSelection;
  const inspected =
    selectedOccurrence && inspectionResult?.inspection
      ? { occurrence: selectedOccurrence, path: inspectionResult.inspection.selected.path }
      : null;
  const closeDetails = () => {
    if (fileSelection) {
      fileReader.closeDetails();
    } else {
      setSelectedOccurrence(null);
      setInspectionResult(null);
    }
    focusRegion("evidence");
  };
  const leaveSubpage = () => {
    setCapturing(false);
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
  const interruptible = running === "workspace";

  // The window's own width decides the sidebar: a labelled 13rem sidebar where
  // there is room, an icon rail below that with the project switcher moved
  // into the page header, never both gone.
  const [frame, setFrame] = useState<HTMLDivElement | null>(null);
  const { width: frameWidth, rem } = useMeasured(frame);
  const compact = frameWidth > 0 && frameWidth / rem < RAIL_BREAKPOINT_REM;
  const narrow = frameWidth > 0 && frameWidth / rem < NARROW_WINDOW_REM;
  useEffect(() => {
    document.documentElement.classList.toggle("narrow-window", narrow);
  }, [narrow]);
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
        ) : capture.recording && !(place === "cases" && subpage === "capture") ? (
          // A capture keeps recording while the person is elsewhere; this
          // returns to it, and its Stop finishes it.
          <OperationIndicator label="Recording" onOpen={() => { go("cases"); setCapturing(true); }} onStop={capture.finish} />
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
            resumable={resumable}
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
            notices={projectNotices}
            busy={busy}
            onOpen={(item) => void openListedProject(item)}
            onLocate={(item) =>
              void locateItem({ context: { project: "", generation: 0 }, ref: item.ref }).then((answer) => {
                setProjectNotices((held) => withNotice(held, item.ref.id, answer));
                return refreshRecent();
              })
            }
            onSettings={(item) => void openListedProject(item).then(() => setEditingProject(true))}
            onReveal={(item) => void revealItem({ context: { project: "", generation: 0 }, ref: item.ref })}
            onForget={(item) =>
              void forgetProject(item.ref.id).then((answer) => {
                setProjectNotices((held) => withNotice(held, item.ref.id, answer));
                return refreshRecent();
              })
            }
            onDelete={(item) => setDeleting({ item, context: { project: item.summary.project?.folder ?? "", project_id: item.ref.id, generation: 0 } })}
            onNew={() => perform("new-project")}
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
          details={verified && subpage === null && caseFlow === null ? { name: caseTitle, open: () => setFileDetails(true) } : undefined}
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
            subpage === "capture"
              ? capture.title
              : verified
                    ? caseFlow !== null
                      ? CASE_FLOW_TITLES[caseFlow]
                      : caseTitle
                    : "Cases"
          }
          actions={
            subpage === "capture" ? (
              capture.actions
            ) : root && subpage === null && !verified ? (
              <>
                <button type="button" disabled={busy} onClick={startCaptureSetup}>
                  Capture
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => setImporting(true)}>
                  Import
                </button>
              </>
            ) : verified && subpage === null && caseFlow === null ? (
              <>
                <Menu
                  label="More case actions"
                  items={caseMenu}
                />
              </>
            ) : null
          }
        >
          {!root ? noProject("cases") : null}
          {capturing && root ? capture.body : null}
          {root ? (
            <ImportFlow
              open={importing}
              root={root}
              context={importContext}
              drafts={drafts}
              busy={busy}
              onClose={() => {
                setImporting(false);
                void leaveImport();
              }}
              onImported={(ref) => {
                setImporting(false);
                void openImportedCase(ref);
              }}
            />
          ) : null}

          {root && subpage === null && verified === null ? (
            <>
              {resumeNotice ? (
                <div className="notice danger" role="alert">
                  {resumeNotice}
                </div>
              ) : null}
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
                revisions={currentProject?.summary.project?.revisions ?? []}
                notices={caseNotices}
                loading={casesLoading}
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
                onLocate={(item) =>
                  void locateItem({ context: projectContext(), ref: item.ref }).then((answer) => {
                    setCaseNotices((held) => withNotice(held, item.ref.id, answer));
                    return refreshCases();
                  })
                }
                onImport={() => setImporting(true)}
                sort={caseListSort}
                onSort={setCaseListSort}
                busy={busy}
              />
            </>
          ) : null}

          {verified && root ? (
            <ViewKey id={verified.identity}>
            <div className="case-view" hidden={subpage !== null}>
              {caseNotice ? (
                <div className="notice danger">
                  <Report indicators={indicators} progress={null} result={caseNotice} />
                  <button type="button" className="action-back-to-listing" onClick={backToProject}>
                    Back to cases
                  </button>
                </div>
              ) : null}
              <div className="case-views" hidden={caseFlow !== null}>
              <TaskTabs label="Case views" id="case-views" tabs={CASE_VIEWS} selected={caseView} onSelect={setView} panels>
                <TaskPanel tabs="case-views" tab="messages" className="task-panel view-panel" shown={caseView === "messages"}>
                  <Report indicators={indicators} progress={running === "case" ? "Verifying the case." : null} result={null} />
                  {evidenceFocus ? (
                    <div className="chips" role="group" aria-label="Applied filters">
                      <span className="chip">
                        Finding evidence
                        <IconButton
                          icon="close"
                          label="Remove Finding evidence"
                          onClick={() => {
                            setEvidenceFocus(null);
                            if (root) void loadMessages(root, shownCase, messageQuery, messageSort, 0);
                          }}
                        />
                      </span>
                      {evidenceFrom === "similar" ? (
                        <button type="button" className="quiet" onClick={() => back()}>
                          Back to similar findings
                        </button>
                      ) : (
                        <button type="button" className="quiet" onClick={() => setView("findings")}>
                          Back to findings
                        </button>
                      )}
                    </div>
                  ) : null}
                  <MessageList
                    result={messages?.result ?? null}
                    rows={messages?.rows ?? []}
                    loading={readingMessages === "messages"}
                    query={messageQuery}
                    onQuery={(query) => {
                      setEvidenceFocus(null);
                      setMessageQuery(query);
                      if (messageView && !views.some((saved) => saved.name === messageView && sameQuery(saved.query, query))) setMessageView("");
                      if (root) void loadMessages(root, shownCase, query, messageSort, 0);
                    }}
                    sort={messageSort}
                    onSort={(sort) => {
                      setMessageSort(sort);
                      if (root) void loadMessages(root, shownCase, messageQuery, sort, 0);
                    }}
                    views={views}
                    view={messageView}
                    onView={(name) => {
                      const saved = views.find((candidate) => candidate.name === name);
                      const query = saved?.query ?? NO_QUERY;
                      setMessageView(saved ? name : "");
                      setMessageQuery(query);
                      if (root) void loadMessages(root, shownCase, query, messageSort, 0);
                    }}
                    onSaveView={async (name) => {
                      if (!root) return { reason: "No project is open." };
                      const answer = await saveView(root, name, messageQuery);
                      if (answer.state !== "completed" && answer.state !== "empty") return { reason: answer.reason ?? "The view was not saved." };
                      setViews(answer.views);
                      setMessageView(name);
                      return null;
                    }}
                    onRenameView={async (from, to) => {
                      if (!root) return { reason: "No project is open." };
                      const answer = await renameView(root, from, to);
                      if (answer.state !== "completed" && answer.state !== "empty") return { reason: answer.reason ?? "The view was not renamed." };
                      setViews(answer.views);
                      setMessageView(to);
                      return null;
                    }}
                    onRemoveView={async (name) => {
                      if (!root) return { reason: "No project is open." };
                      const answer = await removeView(root, name);
                      if (answer.state !== "completed" && answer.state !== "empty") return { reason: answer.reason ?? "The view was not removed." };
                      setViews(answer.views);
                      setMessageView("");
                      return null;
                    }}
                    selected={selectedOccurrence}
                    onInspect={(occurrence) => void inspect(occurrence, evidenceFocus?.find((entry) => entry.occurrence === occurrence)?.field ?? "", 0, -1)}
                    checked={checkedMessages}
                    onCheck={setCheckedMessages}
                    onCreateTest={() => void createTest()}
                    onSendSelected={() => setCaseFlow("replay")}
                    onCreateVariant={() => void seedVariant(chosenRows.map((row) => row.id))}
                    onLoadMore={() => {
                      if (root && messages) void loadMessages(root, shownCase, messageQuery, messageSort, messages.rows.length);
                    }}
                    more={messages?.more ?? null}
                    onRetry={() => {
                      if (root) void loadMessages(root, shownCase, messageQuery, messageSort, 0);
                    }}
                    onImport={() => setImporting(true)}
                    onFields={() => messageFields({ workspace: root ?? "", ...shownCase })}
                    onSearchSettings={() => {
                      setSearchSettings(undefined);
                      if (root) void describeSearchSettings(root, verified.name, verified.identity).then((answer) => setSearchSettings(answer.settings ?? null));
                    }}
                    seed={filterSeed}
                    onSeedUsed={() => setFilterSeed(null)}
                    busy={busy}
                  />
                  <SearchSettingsSheet
                    open={searchSettings !== undefined}
                    settings={searchSettings ?? null}
                    onClose={() => setSearchSettings(undefined)}
                    onSave={async (fields, retention, until) => {
                      if (!root) return { reason: "No project is open." };
                      const answer = await saveSearchSettings({ workspace: root, case: verified.name, identity: verified.identity, fields, retention, retain_until: until });
                      if (answer.state !== "completed") return { reason: answer.reason ?? "The search settings were not saved." };
                      setSearchSettings(undefined);
                      void loadMessages(root, shownCase, messageQuery, messageSort, 0);
                      return null;
                    }}
                  />
                </TaskPanel>
                <TaskPanel tabs="case-views" tab="timeline" className="task-panel view-panel" shown={caseView === "timeline"}>
                  {timeline.toolbar}
                  {timeline.body}
                </TaskPanel>
                <TaskPanel tabs="case-views" tab="findings" className="task-panel view-panel" shown={caseView === "findings"}>
                  {findings.toolbar}
                  {findings.body}
                </TaskPanel>
              </TaskTabs>
              </div>
                {caseFlow === "compare" ? (
                  <div className="view-panel case-flow">
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
                ) : null}
                {caseFlow === "reproduce" ? (
                  <div className="view-panel case-flow">
                    {messages ? (
                      <Reproducer
                        rows={messages.rows}
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
                        parentCase={messages.case}
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
                ) : null}
                {caseFlow === "reduce" ? (
                  <div className="view-panel case-flow">
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
                ) : null}
                {caseFlow === "replay" ? (
                  <div className="view-panel case-flow">
                    <ReplayPanel
                      key={"replay-" + root + verified.identity}
                      workspace={root}
                      caseName={verified.name}
                      identity={verified.identity}
                      rows={messages?.case === verified.name && messages.identity === verified.identity ? chosenRows : []}
                      entries={artifacts}
                      busy={busy}
                      onSent={() => void refreshListing()}
                    />
                  </div>
                ) : null}

            </div>
            </ViewKey>
          ) : null}
        </Page>

        <Page id="similar-findings" shown={place === "similar-findings"} title={similar.title} back={<BackLink label="Findings" onBack={back} />} actions={root ? similar.actions : null}>
          {root ? similar.body : noProject("similar findings")}
        </Page>

        <Page
          id="tests"
          shown={place === "tests"}
          title={testsPlace.kind === "list" && testsView === "suites" ? suites.title : tests.title}
          details={"details" in tests ? tests.details : undefined}
          back={tests.back}
          actions={
            !root ? null : testsPlace.kind === "list" && testsView === "suites" ? (
              <>
                {suites.actions}
                <Menu label="Suite page actions" items={[{ label: "Import suite", onSelect: () => void suites.importSuite() }]} />
              </>
            ) : (
              <>
                {tests.actions}
                {testsPlace.kind === "list" ? <Menu label="Test page actions" items={[{ label: "Import test", onSelect: () => void tests.importTest() }]} /> : null}
              </>
            )
          }
        >
          {!root ? (
            noProject("tests")
          ) : testsPlace.kind === "test" ? (
            tests.body
          ) : (
            <TaskTabs label="Test views" id="tests-views" tabs={TESTS_VIEWS} selected={testsView} onSelect={setView} panels>
              <TaskPanel tabs="tests-views" tab="tests" className="task-panel view-panel" shown={testsView === "tests"}>
                <div className="toolbar list-toolbar">{tests.toolbar}</div>
                {testsPlace.kind === "list" ? tests.body : null}
              </TaskPanel>
              <TaskPanel tabs="tests-views" tab="suites" className="task-panel view-panel" shown={testsView === "suites"}>
                <div className="toolbar list-toolbar">{suites.toolbar}</div>
                {suitesPlace.kind === "list" ? suites.body : null}
              </TaskPanel>
            </TaskTabs>
          )}
        </Page>

        <Page id="new-test" shown={place === "new-test"} title={tests.title} back={tests.back} actions={tests.actions}>
          {root && place === "new-test" ? tests.body : null}
        </Page>

        <Page id="edit-test" shown={place === "edit-test"} title={tests.title} back={tests.back} actions={tests.actions}>
          {root && place === "edit-test" ? tests.body : null}
        </Page>

        <Page
          id="library"
          shown={place === "library"}
          title={libraryObject !== undefined ? libraryPage.title : "Library"}
          back={<BackLink label={libraryObject !== undefined ? "Library" : "Tests"} onBack={back} />}
          actions={
            !root ? null : libraryObject !== undefined ? (
              libraryPage.actions
            ) : (
              <>
                <button type="button" disabled={busy} onClick={() => void importLibrary()}>
                  {libraryView === "checks" ? "Import check group" : libraryView === "profiles" ? "Import profile" : "Import scenario"}
                </button>
                {libraryView !== "profiles" ? (
                  <button type="button" className="primary" disabled={busy} onClick={() => openLibraryObject("new")}>
                    {libraryView === "checks" ? "New check group" : "New scenario"}
                  </button>
                ) : null}
              </>
            )
          }
        >
          {!root ? (
            noProject("library")
          ) : libraryObject !== undefined ? (
            libraryPage.body
          ) : (
            <TaskTabs label="Library views" id="library-views" tabs={LIBRARY_VIEWS} selected={libraryView} onSelect={setView} panels>
              {libraryNotice ? (
                <div className="notice danger" role="alert">
                  {libraryNotice}
                </div>
              ) : null}
              {LIBRARY_VIEWS.map((view) => (
                <TaskPanel key={view.key} tabs="library-views" tab={view.key} className="task-panel view-panel" shown={libraryView === view.key}>
                  <LibraryList
                    kind={LIBRARY_KINDS[view.key]}
                    items={libraryLists[view.key].items}
                    loading={libraryLists[view.key].loading}
                    failure={libraryLists[view.key].failure}
                    busy={busy}
                    onOpen={(item) => openLibraryObject(item.ref.id)}
                    onCreate={() => (view.key === "profiles" ? void importLibrary() : openLibraryObject("new"))}
                    onRetry={() => void libraryLists[view.key].refresh()}
                  />
                </TaskPanel>
              ))}
            </TaskTabs>
          )}
        </Page>

        <Page
          id="suite"
          shown={place === "suite"}
          title={suites.title}
          details={"details" in suites ? suites.details : undefined}
          back={suites.back}
          actions={root ? suites.actions : null}
        >
          {root && place === "suite" ? suites.body : null}
        </Page>

        <Page id="edit-suite" shown={place === "edit-suite"} title={suites.title} back={suites.back} actions={root ? suites.actions : null}>
          {root && place === "edit-suite" ? suites.body : null}
        </Page>

        <Page id="suite-review" shown={place === "suite-review"} title={suites.title} back={suites.back} actions={root ? suites.actions : null}>
          {root && place === "suite-review" ? suites.body : null}
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
                preflightOnArrival={runArrival}
                {...(runEnvironment ? { initialEnvironment: runEnvironment } : {})}
                suiteItem={suiteHandoff && suiteHandoff.workspace === root ? suiteHandoff.handoff : null}
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
          title={environments.title}
          back={environments.back}
          actions={root ? environments.actions : null}
        >
          {opened ? environments.body : noProject("environments")}
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
                onClick={() => void addAttachments({ context: projectContext(), ref: contextCase.ref }).then(attached)}
              >
                Add attachment
              </button>
            ) : null
          }
        >
          {root && contextCase ? (
            <>
              {attachmentNotice ? (
                <div className="notice danger" role="alert">
                  {attachmentNotice}
                </div>
              ) : null}
              <AttachmentsList
                attachments={attachments}
                busy={busy}
                onAdd={() => void addAttachments({ context: projectContext(), ref: contextCase.ref }).then(attached)}
                onRemove={(file) => void removeAttachment({ context: projectContext(), case: contextCase.ref, id: file.id }).then(attached)}
              />
            </>
          ) : (
            noProject("attachments")
          )}
        </Page>

        <Page id="project-files" shown={place === "project-files"} title="Files" back={<BackLink label="Cases" onBack={back} />}>
          {root ? (
            <FilesList
              files={files}
              onOpen={(file) => {
                open({ destination: "inspect-file" });
                fileReader.openPath(childPath(root, file.name));
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

        <Page id="inspect-file" shown={place === "inspect-file"} title={fileReader.title} back={<BackLink label="Tools" onBack={back} />} actions={fileReader.actions}>
          {fileReader.body}
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
          {root ? (
            <SampleFixture
              context={libraryContext}
              busy={busy}
              onOpenCase={(_ref, entry) => {
                void verifyCase(root, entry);
              }}
            />
          ) : null}
          {root ? (
            <>
              <SyntheticFamilies workspace={root} busy={busy} />
              <ScenarioLibraryCheck workspace={root} busy={busy} />
            </>
          ) : null}
          <section className="sample-section" aria-label="Demo report">
            <h2>Demo report</h2>
            <SyntheticPackets onRefresh={() => void refreshListing()} />
          </section>
        </Page>

        <Page id="benchmarks" shown={place === "benchmarks"} title="Benchmarks" back={<BackLink label="Tools" onBack={back} />}>
          <PerformanceCorpus busy={busy} indicators={indicators} request={corpusRequest} />
        </Page>

        <Page id="settings" shown={place === "settings"} title="Settings">
          <Categories label="Settings categories" categories={SETTINGS_VIEWS} selected={settingsView} onSelect={setView}>
            <TaskPanel tabs="settings-views" tab="general" className="task-panel view-panel" shown={settingsView === "general"}>
              <GeneralView
                preferences={preferences}
                themes={described?.themes ?? ["system"]}
                scales={described?.text_scales ?? [100]}
                version={described?.version ?? ""}
                build={described?.build}
                onCheckUpdate={() => {
                  setStartUpdate(true);
                  openSettings("storage");
                }}
              />
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="license" className="task-panel view-panel" shown={settingsView === "license"}>
              <OperationAccess request={setupRequests.portal} onHandled={setupHandled("portal")} onConfigured={configured("portal", "portal")} />
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="team" className="task-panel view-panel" shown={settingsView === "team"}>
              <HubPanel
                workspace={root}
                entries={artifacts}
                request={setupRequests.team}
                onHandled={setupHandled("team")}
                onConfigured={configured("team", "hub:client")}
                operatorRequest={setupRequests.operator}
                onOperatorHandled={setupHandled("operator")}
                onOperatorConfigured={configured("operator", "hub:operator")}
              />
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="runners" className="task-panel view-panel" shown={settingsView === "runners"}>
              <RunnerPanel request={setupRequests.runner} configPath={runnerPath} onHandled={setupHandled("runner")} onConfigured={configured("runner", "runner:config")} seed={runnerSeed} />
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="security" className="task-panel view-panel" shown={settingsView === "security"}>
              {place === "settings" && settingsView === "security" ? (
                <SecurityView
                  root={root}
                  projectName={projectName}
                  returnTo={securityReturn}
                  onReturned={() => setSecurityReturn(undefined)}
                  busy={busy}
                  operations={described?.privacy.operations ?? []}
                  onOpen={openConnection}
                  onEncryption={() => open({ destination: "encryption" })}
                />
              ) : null}
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="storage" className="task-panel view-panel" shown={settingsView === "storage"}>
              {place === "settings" && settingsView === "storage" ? (
                <StorageView
                  root={root}
                  projectName={projectName}
                  projects={projects}
                  context={storageContext}
                  busy={busy}
                  onOpenProject={(folder) => void openFolder(() => openWorkspace(folder))}
                  startUpdate={startUpdate}
                  onUpdateStarted={() => setStartUpdate(false)}
                />
              ) : null}
            </TaskPanel>
          </Categories>
        </Page>

        <Page
          id="encryption"
          shown={place === "encryption"}
          title="Encryption"
          back={<BackLink label="Settings" onBack={back} />}
          actions={
            root ? (
              <>
                {encryption.actions}
                <Menu label="More encryption actions" items={[{ label: "Packages", onSelect: () => setPackagesOpen(true) }]} />
              </>
            ) : null
          }
        >
          {root ? encryption.body : noProject("encryption")}
          <Modal open={packagesOpen && root !== null} title="Packages" size="wide" onClose={() => setPackagesOpen(false)}>
            <ProtectionPanel workspace={root} entries={artifacts} onRefresh={() => void refreshListing()} />
          </Modal>
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
        {/* Shown alone in a compact window, the details carry the project
            switcher the hidden page header holds, so it is never out of reach. */}
        {detailOnly && compact && switcher ? <div className="header-switcher detail-switcher">{switcher}</div> : null}
        {fileSelection ? (
      <>
        {fileReader.details}
      </>
    ) : findingShown ? (
      <>
        {/* Shown alone, the finding offers the way back to its list. */}
        {detailOnly ? <BackLink label="Findings" onBack={findings.close} /> : null}
        {findings.details}
      </>
    ) : linkShown ? (
      <>
        {/* Shown alone, the link offers the way back to the Timeline. */}
        {detailOnly ? <BackLink label="Timeline" onBack={timeline.close} /> : null}
        {timeline.details}
      </>
    ) : (
      <>
        {verified ? (
          <MessageReader
            result={inspectionResult}
            {...(selectedRow ? { kind: selectedRow.kind, source: selectedRow.source_name || selectedRow.source_id } : {})}
            loading={running === "inspect"}
            busy={busy}
            onInspect={(path, nodeOffset, byteOffset, rawOffset) => (selectedOccurrence ? inspect(selectedOccurrence, path, nodeOffset, byteOffset, undefined, revealed, rawOffset) : Promise.resolve(null))}
            onReveal={(next) => {
              setRevealed(next);
              const at = inspectionResult?.inspection;
              if (selectedOccurrence) void inspect(selectedOccurrence, at?.selected.path ?? "", at?.node_offset ?? 0, at?.byte_offset ?? -1, undefined, next);
            }}
            onFilterByField={(selector: string, value: string | null, state: FieldState) => {
              setView("messages");
              setFilterSeed({ selector, value, state });
            }}
            onClose={closeDetails}
            {...(detailOnly ? { backLabel: caseView === "timeline" ? "Timeline" : "Messages", onBack: closeDetails } : {})}
          />
        ) : null}
      </>
    )}
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
    // The shown object's own menu first. Each runs exactly what its menu item
    // does, so Delete, Remove, Reset or Send opens its review and nothing more.
    ...[...objectActions.values()].flatMap(({ object, items }) =>
      items()
        .filter((item) => !item.disabled)
        .map((item) => ({ id: `object-${object}-${item.label}`, label: item.label.replace(/…$/, ""), object, run: item.onSelect })),
    ),
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

  // An action the open object offers is listed once, as the object's.
  const paletteShown = paletteEntries.filter((entry, at) => paletteEntries.findIndex((other) => other.label === entry.label) === at);

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
          <ReturnAnchor.Provider value={returnAnchor}>
          <PaletteActionsContext.Provider value={registerActions}>
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
          </PaletteActionsContext.Provider>
          </ReturnAnchor.Provider>
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
                <dd>
                  <DisplayTerm map={PROVENANCES} code={verified.provenance} />
                </dd>
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
                    <span className="reason">{match.field === "content" ? match.selector : term(SEARCH_FIELDS, match.field).text}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : null}
        </Modal>

        <CaseFacts item={caseFacts} revisions={currentProject?.summary.project?.revisions ?? []} onClose={() => setCaseFacts(null)} />
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
            retain={{ workspace: root ?? "", item: { project_id: currentProject.ref.id, ref: currentProject.ref }, restored: restored?.kind === "project" ? restored : null }}
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
                setRestored(null);
                await refreshRecent();
                // The switcher and every heading name the project as it now reads.
                if (root) setInvestigation(await openProjectOverview(root));
                return;
              }
              const referring = answer.problems
                .filter((problem) => problem.referring && problem.referring.length > 0)
                .map((problem) => ({ revision: problem.field.replace(/^revisions\./, ""), cases: problem.referring!.map((entry) => entry.name) }));
              return { ...saveFailure(answer, { name: "project-name", owner: "project-owner" }), ...(referring.length > 0 ? { referring } : {}) };
            }}
            onClose={() => {
              setEditingProject(false);
              setRestored(null);
            }}
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
              sources: caseTask.item.summary.case?.sources ?? [],
            }}
            revisions={currentProject?.summary.project?.revisions ?? []}
            retain={{
              workspace: root ?? "",
              item: currentProject ? { project_id: currentProject.ref.id, ref: caseTask.item.ref } : undefined,
              restored: restored?.kind === "case" ? restored : null,
            }}
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
                    ...(details.sources.length > 0 ? { sources: details.sources } : {}),
                  },
                },
              });
              if (answer.outcome === "saved") {
                setCaseTask(null);
                setRestored(null);
                await refreshCases();
                return;
              }
              return saveFailure(answer, { name: "case-name", owner: "case-owner", status: "case-status", "case.sources": "case-sources" });
            }}
            onClose={() => {
              setCaseTask(null);
              setRestored(null);
            }}
          />
        ) : null}

        <DeleteSourceSheet
          open={deleting !== null}
          context={() => deleting?.context ?? projectContext()}
          item={deleting?.item.ref ?? null}
          onClose={() => setDeleting(null)}
          onDone={() => {
            void refreshRecent();
            if (root) void refreshCases();
          }}
        />

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
            note={
              noteEditing.note
                ? { id: noteEditing.note.id, name: noteEditing.note.name, content: noteEditing.note.content, about: noteEditing.note.case?.id ?? "" }
                : { name: "", content: "", about: place === "case-notes" && contextCase ? contextCase.ref.id : "" }
            }
            subjects={[
              { id: "", name: currentProject?.name ?? "This project" },
              // The case a note is about stays its subject even when it cannot be read now.
              ...cases
                .filter((item) => item.availability === "available" || item.ref.id === noteEditing.note?.case?.id || item.ref.id === restored?.item?.ref.id)
                .map((item) => ({ id: item.ref.id, name: item.name })),
            ]}
            retain={{
              workspace: root ?? "",
              item: (about) => {
                if (!currentProject) return undefined;
                const subject = cases.find((item) => item.ref.id === about)?.ref;
                return { project_id: currentProject.ref.id, ref: subject ?? currentProject.ref };
              },
              restored: restored?.kind === "note" ? restored : null,
            }}
            onSave={async (note) => {
              const caseRef = note.about !== "" ? (cases.find((item) => item.ref.id === note.about)?.ref ?? noteEditing.note?.case) : undefined;
              const answer = await saveNoteItem({
                context: projectContext(),
                intent_id: newIntentId(),
                note: { ...(note.id ? { id: note.id } : {}), name: note.name, content: note.content, ...(caseRef ? { case: caseRef } : {}) },
              });
              if (answer.state !== "completed") return { reason: answer.reason ?? "The note was not saved.", field: "note-name" };
              setNoteEditing(null);
              setRestored(null);
              await refreshNotes();
            }}
            onClose={() => {
              setNoteEditing(null);
              setRestored(null);
            }}
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

        {usingInTest ? (
          <UseInTestSheet
            context={libraryContext}
            group={usingInTest}
            onClose={() => setUsingInTest(null)}
            onPick={(test) => {
              setPendingCheckGroup(usingInTest.ref);
              routeTo({ type: "go", to: { destination: "edit-test", objectId: test.ref.id }, leaving: leaving() });
            }}
          />
        ) : null}

        <CommandPalette open={paletteOpen} entries={paletteShown} onClose={() => setPaletteOpen(false)} />
      </VocabularyContext.Provider>
    </IndicatorsContext.Provider>
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
  "redact-policy": { route: { destination: "share-report" } },
  "redact-inventory": { route: { destination: "share-report" } },
  "reproducer-plan": { case: "reproduce" },
  import: { import: true },
  "observation-source": { observe: true },
  "observation-window": { observe: true },
};

/** The sheets a retained draft of their object reopens with it. */
const SHEET_DRAFTS = new Set(["case", "project", "note"]);

/** Whether an editor of this window takes a retained draft back. A draft of
 * an earlier release's editor, or a note that names nothing it is about, is
 * offered only for Discard. */
function resumable(draft: EditorDraft): boolean {
  if (SHEET_DRAFTS.has(draft.kind)) return draft.item !== undefined;
  // A test draft reopens only in the editor that wrote it; an earlier
  // release's test drafts are offered for Discard.
  if (draft.kind === "test-draft") return draft.content_schema === TEST_EDITOR_DRAFT;
  if (draft.kind === "suite-editor") return draft.content_schema === SUITE_EDITOR_DRAFT;
  return draft.kind in DRAFT_PLACES;
}

/** A row's notices once an answer about it came back: its reason when it was
 * refused, and nothing once it was done or cancelled. */
function withNotice(held: Record<string, string>, id: string, answer: { state: State; reason?: string }): Record<string, string> {
  const next = { ...held };
  if (answer.state === "cancelled" || answer.state === "completed" || answer.state === "empty") delete next[id];
  else if (answer.reason) next[id] = answer.reason;
  return next;
}

/** One entry of a folder, joined as the folder's own paths are written. */
function childPath(folder: string, name: string): string {
  const separator = folder.includes("\\") && !folder.includes("/") ? "\\" : "/";
  return folder.replace(/[\\/]+$/, "") + separator + name;
}
