import type {ReactNode} from "react";
import type { ValueMapEditorDraft } from "./bindings";
import { ValueMaps } from "./ValueMaps";
import { InterfaceRequirements } from "./InterfaceRequirements";
import { FieldValues } from "./FieldValues";
import { DiagnosticsSheet, HelpTopics, SearchHelpSheet, useHelpArticle } from "./Help";
import { DemoTask } from "./DemoTask";
import { useBenchmarks } from "./Benchmarks";
import { useSchedules } from "./Schedules";
import { AdministratorSetup } from "./OperationAccess";
import { ComputerLicense } from "./ComputerLicense";
import { GeneralView, SecurityView, usePreferences, type ConnectionRoute } from "./Settings";
import { useEncryption } from "./Encryption";
import { HubPanel } from "./HubPanel";
import { CIResultsSheet } from "./CISheets";
import { RunnerPanel } from "./RunnerPanel";
import { useRunComparison } from "./RunComparison";
import { SendReview, sendTitle, type SendRequest } from "./RunPanel";
import { useRunPage } from "./RunExplanation";
import { useRunActivity, useRuns, type RunsPlace } from "./Runs";
import { useReportPage, useReports, type ReportSeed, type ReportsPlace } from "./Reports";
import { SHARE_DRAFT_KIND, useShareReport, type ShareStep } from "./ShareReport";
import { SupportSummarySheet } from "./SupportSummary";
import { useEncryptedPackages } from "./EncryptedPackages";
import { useShareTemplates } from "./ShareTemplates";
import { CONNECTED_SUITE_EDITOR_DRAFT, SUITE_EDITOR_DRAFT, SUITE_VIEWS, useSuites, type SuiteRunHandoff, type SuitesPlace, type SuiteView } from "./Suites";
import { environmentPlace, useEnvironments } from "./Environments";
import { onRetentionResult } from "./drafting";
import { IndicatorsContext, useLifecycle, useWindowBusy } from "./lifecycle";
import { cloneElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactElement } from "react";
import { useLayoutEffect } from "react";
import {
  cancel,
  createNamedProject,
  RequestScope,
  openNamedProject,
  listWholeCatalog,
  openItemDraft,
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
  filters as readFilters,
  demoProgress,
  openDemoProject,
  runPractice,
  type DemoProgress,
  type DemoStepID,
  type HelpActionID,
  editorDrafts as readEditorDrafts,
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
  inspectOccurrence,
  type InspectionResult,
  type HL7ReferenceSelection,
  type ExchangeOrigin,
  type ExchangeTestProvenance,
  openProjectOverview,
  openWorkspace,
  recordView,
  workingSession,
  readReferenceCatalog,
  readHL7ReferenceSelection,
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
  type DatasetValue,
  onFileDrop,
} from "./bindings";
import { useVariantEditor, VARIANT_DRAFT_KIND, type VariantSource } from "./Variant";
import { useCaseComparison, type ComparedCase, type ComparisonTab } from "./CaseComparison";
import { useMinimize, useMinimizeActivity } from "./Minimize";
import { useTimeline } from "./Timeline";
import { MessageReader } from "./Inspector";
import { ExchangePanel } from "./ExchangePanel";
import { SelectedMessagesExportPanel, SourceExportHistory } from "./SelectedMessagesExport";
import { MessageList, NO_QUERY, sameQuery, SearchSettingsSheet, type FilterSeed } from "./Messages";
import { LibraryList, UseInTestSheet, importDraft, useCheckGroup, useLibraryItems, type LibraryKind } from "./Library";
import { PacksSheet, useProfile } from "./ProfileLibrary";
import { SCENARIO_DRAFT_KIND, SCENARIO_DRAFT_SCHEMA, scenarioDraftObject, useScenario } from "./ScenarioLibrary";
import { SampleFixture, ScenarioLibraryCheck, SyntheticFamilies } from "./SampleData";
import { SyntheticPackets } from "./SyntheticPackets";
import { useTests,isTestWorkspaceView, type TestsPlace } from "./Tests";
import { CONNECTED_EDITOR_DRAFT, EXCHANGE_EDITOR_DRAFT, TEST_EDITOR_DRAFT } from "./TestEditor";
import { useFindings, useSimilarFindings, type EvidenceRef } from "./Findings";
import { Report, Separator, Status } from "./shell";
import { CommandPalette, isMac, shortcut, type PaletteEntry } from "./CommandPalette";
import { ReturnAnchor, firstShownRow, type SortState } from "./DataTable";
import { ViewKey, forgetViewState } from "./viewstate";
import { sidebarOf, workspaceDestination, useRoutes, viewOf, isPlace, type ReturnContext, type Route, type NavigationEvidence } from "./routes";
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
  READER_COMPACT_WINDOW_REM,
  READER_COMPACT_SIDEBAR_REM,
  MESSAGE_BROWSER_REM,
  MESSAGE_BROWSER_COMPACT_REM,
  READER_REFERENCE_REM,
  READER_REFERENCE_COMPACT_REM,
} from "./geometry";
import { VocabularyContext } from "./vocabulary";
import { useSourceSelection } from "./sourceSelection";
import { SelectedMessageComparison } from "./SelectedMessageComparison";
import { ReceiveConfigurations } from "./ReceiveConfigurations";
import { CaptureSourceContext } from "./CaptureSourceContext";
import { ImportFlow, importDrop } from "./Import";
import { useCapture } from "./Capture";
import { DeleteSourceSheet, StorageView } from "./Storage";
import { useFileReader } from "./RawInspection";
import chevronsAsset from "./assets/workbench/chevrons.svg";
import moreAsset from "./assets/workbench/more.svg";
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
  | "practice"
  | "recent"
  | "sample";

/** The views inside a page. Each page keeps every view mounted, so an edit in
 * one survives looking at another. */
type CaseView = "messages" | "timeline" | "findings";
/** What a person does to a case, each on its own page with a way back. */
type CaseFlow = "variant" | "compare" | "field-values" | "requirements" | "value-maps";
type TestsView = "tests" | "suites";
type LibraryView = "checks" | "profiles" | "scenarios";
type SettingsView = "general" | "license" | "team" | "runners" | "security" | "storage";

const CASE_VIEWS: { key: CaseView; label: string }[] = [
  { key: "messages", label: "Messages" },
  { key: "timeline", label: "Timeline" },
  { key: "findings", label: "Findings" },
];
const TESTS_VIEWS: { key: TestsView; label: string }[] = [
  { key: "tests", label: "Test cases" },
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
  practice: "Running the demo test…",
  recent: "Updating recent projects…",
  sample: "Importing the demo fixtures…",
};

const NO_SELECTED_ROWS:import("./bindings").MessageRow[]=[];

export default function App() {
  const [described, setDescribed] = useState<Shell | null>(null);
  const [windowState, setWindowState] = useState<State>("busy");
  const [windowReason, setWindowReason] = useState<string | undefined>(undefined);

  // One operation runs at a time here as well as in the facade, and it holds
  // the window's one slot: while it runs, the rest of the window is
  // unavailable rather than answered busy.
  // Reading a case's messages is a background read of its own: it never holds
  // the window's one slot, so it never disables what a person is typing in, and
  // it never moves focus.
  const { running: readingMessages, run: readMessagesIn } = useLifecycle<"messages">({ background: true });
  const { running, run } = useLifecycle<Running>({
    window: true,
    names: { practice: "practice" },
  });
  // Each counts the palette's requests to open that screen in the inspector.
  const [rawRequest, setRawRequest] = useState(0);
  const [readerReferenceRequest,setReaderReferenceRequest]=useState(0);
  const [readerDensityOverride,setReaderDensityOverride]=useState<boolean|null>(null);
  const [workspace, setWorkspace] = useState<WorkspaceResult | null>(null);
  const [evidence, setEvidence] = useState<CaseResult | null>(null);
  const [sourceKind, setSourceKind] = useState<"case" | "file">("case");
  const [fileNavigation, setFileNavigation] = useState<Partial<import("./bindings.gen").ViewNavigation> | null>(null);
  const [sessionReady, setSessionReady] = useState(false);
  const pendingSessionOwner=useRef<number|null>(null);
  const [pendingSession, setPendingSession] = useState<import("./bindings").View | null>(null);
  const [sessionNotice, setSessionNotice] = useState<string | null>(null);
 const [sessionWriteNotice,setSessionWriteNotice]=useState<string|null>(null);
  const restoredScroll = useRef<number | null>(null);
  const restoreStarted = useRef(false);
  const [scrollRevision, setScrollRevision] = useState(0);
  const [investigation, setInvestigation] = useState<ProjectOverviewResult | null>(null);
  const [found, setFound] = useState<SearchResult | null>(null);
  const [inspectionResult, setInspectionResult] = useState<InspectionResult | null>(null);
  const referenceCatalog = useRef("");
  const referenceIdentity = useRef("");
  const referenceSelection = useRef<HL7ReferenceSelection | undefined>(undefined);
  const [exchangeSource, setExchangeSource] = useState<{ project: string; entry: string; identity: string; ref: ItemRef } | null>(null);
  const [exchangeDetails,setExchangeDetails]=useState<{key:string;node:ReactNode}|null>(null);
  const [exchangeUIRequest,setExchangeUIRequest]=useState<{kind:"setup"|"history";serial:number}|null>(null);
  const receiveExchangeContext=useCallback((node:ReactNode|null,scope:{caseID:string;identity:string;occurrence:string}|null)=>setExchangeDetails(node&&scope?{key:JSON.stringify([scope.caseID,scope.identity,scope.occurrence]),node}:null),[]);
  const [requestedExchange, setRequestedExchange] = useState<{ project: string; origin: ExchangeOrigin } | null>(null);
  const [selectedExportOpen, setSelectedExportOpen] = useState(false);
  const [sourceExportsOpen, setSourceExportsOpen] = useState(false);
  const [ciResultsOpen,setCIResultsOpen]=useState(false);
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
  const messageSelectionOwner = useRef("");
  // The messages a finding's evidence names, shown on their own until cleared.
  const [evidenceFocus, setEvidenceFocus] = useState<EvidenceRef[] | null>(null);
  // Where the evidence shown was opened from: this case's Findings, or a
  // comparison of similar findings, which Back returns to.
  const [evidenceFrom, setEvidenceFrom] = useState<"findings" | "similar" | "run">("findings");
  const [revealed, setRevealed] = useState(false);
  const [filterSeed, setFilterSeed] = useState<FilterSeed | null>(null);
  const [searchSettings, setSearchSettings] = useState<SearchSettings | null | undefined>(undefined);
  // The reviewed send open now (#555): a test, a suite, chosen messages, the
  // rest of an interrupted run or a reviewed test. Its Send is the only thing
  // that sends.
  const [sendRequest, setSendRequest] = useState<SendRequest | null>(null);
 const [reviewSetup,setReviewSetup]=useState<{project:string;request:SendRequest}|null>(null);
  // A review whose license refusal sent the person to activate: it is
  // prepared again, fresh, once they leave Settings.
  // Counts the runs that ended, so Runs reads its list again.
  const [runsEnded, setRunsEnded] = useState(0);
  // What Create report hands Reports: a run, its case and its test version.
  const [reportSeed, setReportSeed] = useState<ReportSeed | null>(null);
  // The demo task over the open project, read back from what it holds, and
  // the steps whose read results this window saw.
  const [demo, setDemo] = useState<DemoProgress | null>(null);
  const [demoSeen, setDemoSeen] = useState<ReadonlySet<DemoStepID>>(new Set());
  const [demoShown, setDemoShown] = useState(true);
  const [demoNotice, setDemoNotice] = useState<string | null>(null);
  const [seedTest, setSeedTest] = useState<{ case: ItemRef; test: DemoProgress["test"] } | null>(null);
  const [helpSheet, setHelpSheet] = useState<null | "search" | "diagnostics">(null);
  // The Support summary sheet: of one report, or of one chosen in it.
  const [supportSheet, setSupportSheet] = useState<{ report: ItemRef | null } | null>(null);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const [watchedRun, setWatchedRun] = useState("");
  const [drafts, setDrafts] = useState<EditorDraft[] | null>(null);
  const [capturing, setCapturing] = useState(false);
  const [importing, setImporting] = useState(false);
 const [looseRetention,setLooseRetention]=useState<import("./bindings").ImportInvestigation | null>(null);
 const retentionStarted=useRef("");
 const [captureContextSource,setCaptureContextSource]=useState<ItemRef|null>(null);
 const [receivingSetup,setReceivingSetup]=useState(false);
 const [selectedComparison,setSelectedComparison]=useState(false);
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
  const [setupRequests, setSetupRequests] = useState({ team: 0, operator: 0, portal: 0, runner: 0 });
  // The inspector width each project was last given, in rem. The shown width
  // is clamped to the room there is now without overwriting the wider choice.
  const [inspectorWidths, setInspectorWidths] = useState<Record<string, number>>({});
  const [messageBrowserWidths, setMessageBrowserWidths] = useState<Record<string, number>>({});

  // Where the window is: one route, with the way back kept per destination.
  // Only the shown destination is mounted; what a page holds unsaved is kept
  // in the view state for this project.
  const { route, dispatch: routeTo } = useRoutes({ destination: "home" });
  const place = route.destination;
  const destination = workspaceDestination(route);
  const captureCollection = place === "cases" && route.objectId === undefined;
  const currentRoute = useRef<Route>(route);
  const navigationSerial = useRef(0);
  currentRoute.current = route;
  const scenarioNavigation = useRef<((exit: () => void) => void) | null>(null);
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
    if (route.destination === "cases" && returned.selection !== undefined) setSelectedCase(returned.selection);
    const body = document.querySelector<HTMLElement>(`.page[data-page="${route.destination}"] .page-body`);
    if (body && returned.scrollTop !== undefined) body.scrollTop = returned.scrollTop;
  }, [route]);
  const [caseFlow, setCaseFlow] = useState<CaseFlow | null>(null);
  const [fieldValuesContext, setFieldValuesContext] = useState<{
    workspace: string; case: string; identity: string; selector: string; label?: string;
    occurrence: string | null; path: string; nodeOffset: number; byteOffset: number; scrollTop: number;
  } | null>(null);
  const [fieldValuesInspecting, setFieldValuesInspecting] = useState(false);
  const [requirementsContext, setRequirementsContext] = useState<{
    workspace: string; case: string; identity: string; source: ItemRef; occurrence: string; selector: string; edition:string;
    originOccurrence: string | null; originPath: string; nodeOffset: number; byteOffset: number; scrollTop: number; view: CaseView;
  } | null>(null);
  // Create variant and Compare (#558): the case each was started for, the
  // messages or the other case chosen first, and a serial per start.
  const [variantFlow, setVariantFlow] = useState<{ source: VariantSource; seed: string[]; serial: number } | null>(null);
  const [compareFlow, setCompareFlow] = useState<{ current: ComparedCase; other: ItemRef | null; tab?: ComparisonTab | undefined; serial: number } | null>(null);
  // The open case as the project names it and, for a variant, the case it
  // was made from.
  const [openObject, setOpenObject] = useState<{ ref: ItemRef; name: string; parent: ItemRef | null; project:string;entry:string;identity:string } | null>(null);
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
  // Why the project's files could not be listed, when they could not.
  const [filesFailure, setFilesFailure] = useState<string | null>(null);
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
  const [casesFailure, setCasesFailure] = useState<string | null>(null);
  // The case whose recorded details are shown.
  const [caseFacts, setCaseFacts] = useState<CatalogItem | null>(null);
  // A retained draft being resumed: once its project is open, the sheet it
  // belongs to opens with it.
  const [resuming, setResuming] = useState<EditorDraft | null>(null);
  const [restored, setRestored] = useState<EditorDraft | null>(null);
  // Why a draft chosen to resume found nothing to reopen.
  const [unavailableMapDraft,setUnavailableMapDraft]=useState<{draft:EditorDraft;reason:string}|null>(null);
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

  // The demo task is read back out of the open project rather than
  // remembered; a project that is not the demo has none.
  const refreshDemo = useCallback(async (folder: string | null) => {
    if (!folder) {
      setDemo(null);
      return;
    }
    const answer = await demoProgress({ project: folder, generation: 0 });
    setDemo(answer.state === "completed" && answer.demo ? answer.demo : null);
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
  const view = useCallback((run: string): import("./bindings").View => {
    const source = evidence?.case;
    const inspected = inspectionResult?.inspection;
    const origin = caseFlow === "field-values" ? fieldValuesContext : (caseFlow === "requirements" || caseFlow === "value-maps") && requirementsContext ? { occurrence: requirementsContext.originOccurrence, path: requirementsContext.originPath, nodeOffset: requirementsContext.nodeOffset, scrollTop: requirementsContext.scrollTop } : null;
    const occurrence = origin ? origin.occurrence : selectedOccurrence;
    const body = document.querySelector<HTMLElement>(`.page[data-page="${route.destination}"] .page-body`);
    const projectIdentity = projects.find((project) => project.summary.project?.folder === workspaceRoot)?.ref.id;
    const navigation: import("./bindings.gen").ViewNavigation = {
      destination: route.destination, source_kind: sourceKind,
      ...(workspaceRoot && projectIdentity ? { project_identity: projectIdentity } : {}),
      ...(route.objectId ? { object: route.objectId } : {}),
      ...(route.view ? { local_view: route.view } : {}),
      ...(source && workspaceRoot ? { source_identity: source.identity } : {}),
      ...(source && occurrence ? { occurrence } : {}),
 ...(source && sourceKind==="case" && checkedMessages.size ? {checked_occurrences:[...checkedMessages]} : {}),
      ...(source && origin ? { field_path: origin.path, node_offset: origin.nodeOffset } : source && inspected ? { field_path: inspected.fhir?.selected?.field.id ?? inspected.selected.path, node_offset: inspected.node_offset } : {}),
      ...(messageView ? { filter: messageView } : {}),
      ...(messageSort?.column === "time" ? { sort: messageSort.direction === "ascending" ? "time-ascending" : "time-descending" } : {}),
      scroll_top: Math.trunc(origin?.scrollTop ?? body?.scrollTop ?? 0),
      ...(referenceCatalog.current && referenceIdentity.current ? { reference_path: referenceCatalog.current, reference_identity: referenceIdentity.current, ...(inspected?.reference?.edition ? { reference_edition: inspected.reference.edition } : {}) } : {}),
      ...(referenceSelection.current ? { reference_selection: referenceSelection.current } : {}),
      ...(sourceKind === "file" && fileNavigation ? fileNavigation : {}),
    };
    return { workspace: workspaceRoot, region: focused, case: workspaceRoot === "" ? "" : selected ?? "", run, navigation };
  }, [workspaceRoot, focused, selected, route, sourceKind, evidence?.case, selectedOccurrence, inspectionResult, checkedMessages, messageView, messageSort, fileNavigation, scrollRevision, projects, caseFlow, fieldValuesContext, requirementsContext]);

  const recording = useRef(false);
  const pendingRecord = useRef<ReturnType<typeof view> | null>(null);
  const flushDone = useRef<Promise<void>>(Promise.resolve());
  const lastRecordedView = useRef<{ view: import("./bindings").View; result: import("./bindings").SessionResult } | null>(null);
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
          const result=await recordView(next);
          lastRecordedView.current = { view: next, result };
 if(result.state!=="completed" && result.state!=="empty")setSessionWriteNotice(result.reason??"The current navigation could not be retained for restart. Reduce the selection or retry after storage is available.");
 else setSessionWriteNotice(null);
        }
      } finally {
        recording.current = false;
      }
    })();
    return flushDone.current;
  }, []);

  useEffect(() => {
    if (!sessionReady || workspaceRoot === "" && watchedRun === "" && !fileNavigation?.file) {
      return;
    }
    void pushRecord(view(watchedRun));
  }, [pushRecord, view, watchedRun, sessionReady, fileNavigation?.file]);

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
    const recorded = lastRecordedView.current;
    if (!recorded || recorded.view.workspace !== workspaceRoot || recorded.view.run !== folder || recorded.result.state !== "completed") {
      return recorded?.result.reason ?? "The recovery location could not be recorded. The reviewed action has not started.";
    }
  }, [pushRecord, view, workspaceRoot]);

  // Whether an operation holds the window's one slot: this window's own, or a
  // screen's whose call holds the facade, such as the raw inspection and the
  // performance corpus.
  const busy = useWindowBusy();
  const fileReader = useFileReader({
    busy,
    request: rawRequest,
    onNavigation: setFileNavigation,
    onCancelled: () => {
      if (currentRoute.current.destination === "inspect-file") routeTo({ type: "back" });
    },
  });
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
 const sourceSelection=useSourceSelection(sourceKind==="case" && root && verified ? {root,entry:verified.name,identity:verified.identity}:null,checkedMessages,messages?.rows??NO_SELECTED_ROWS);
  const selectedSourceRef = exchangeSource?.project === root && exchangeSource.entry === verified?.name && exchangeSource.identity === verified?.identity ? exchangeSource.ref : null;
  useEffect(() => { setSelectedExportOpen(false); setSourceExportsOpen(false);setCaptureContextSource(null);setReceivingSetup(false);setSelectedComparison(false);setCIResultsOpen(false); }, [root, verified?.name, verified?.identity]);

  const focusRegion = useCallback((region: RegionId, afterRead = false) => {
    // A navigation read may finish after the person opened a sheet. Its
    // completion must not take keyboard input away from that modal.
    if (afterRead && document.querySelector("dialog[open]")) return;
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
    setFieldValuesContext(null);
    setRequirementsContext(null);
    setFieldValuesInspecting(false);
    referenceCatalog.current = "";
    referenceIdentity.current = "";
    referenceSelection.current = undefined;
    setEvidence(null);
    setMessages(null);
    setMessageQuery(NO_QUERY);
    setMessageSort(null);
    setMessageView("");
    setCheckedMessages(new Set());
    messageSelectionOwner.current = "";
    setRevealed(false);
    setFilterSeed(null);
    setInspectionResult(null);
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
    async (operation: () => Promise<WorkspaceResult>, navigationOwner?:number) => {
      setOpenNotice(null);
      let opened = false;
      await run("workspace", async () => {
        const result = await operation();
        if(navigationOwner!==undefined && navigationOwner!==navigationSerial.current)return;
        if (result.workspace) {
          navigationSerial.current += 1;
          const openedOwner=navigationSerial.current;
          clearWorkspace();
          setSelected(null);
          setWorkspace(result);
          // A project opened is a new history: nothing selected, typed or
          // revealed in the last one comes with it.
          routeTo({ type: "project", projectId: result.workspace.root, to: { destination: "cases" } });
          setCaseFlow(null);
          setDemoSeen(new Set());
          setDemoShown(true);
          setDemoNotice(null);
          setImporting(false);
          setCapturing(false);
          setCaptureBinding(null);
          opened = true;
          await refreshDemo(result.workspace.root);
          if(navigationOwner!==undefined && navigationSerial.current!==openedOwner)return;
          // A folder that holds a project opens as that project: its cases and
          // settings are read now rather than behind another button.
          if (result.workspace.artifacts.some((artifact) => artifact.kind === "project")) {
            const project=await openProjectOverview(result.workspace.root);
            if(navigationOwner!==undefined && navigationSerial.current!==openedOwner)return;
            setInvestigation(project);
            // Opening records it among this viewer's projects, newest first.
            await openNamedProject(result.workspace.root);
          }
        } else {
          setOpenNotice(result);
        }
      });
      await refreshRecent();
      if (opened) {
        focusRegion("evidence", true);
      }
      return opened;
    },
    [clearWorkspace, focusRegion, refreshDemo, refreshRecent, run],
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
        if (result.state === "completed" || result.state === "empty") {
          const owner = JSON.stringify([folder, open.case, open.identity]);
          if (messageSelectionOwner.current !== owner) {
            setCheckedMessages(new Set());
            messageSelectionOwner.current = owner;
          }
        }
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
    async (folder: string, name: string, options?: { skipAutoGrid?: boolean; navigationOwner?: number; expectedIdentity?: string }): Promise<CaseResult | null> => {
      setCaseNotice(null);
      let autoIndex: string | null = null;
      let outcome: CaseResult | null = null;
      await run("case", async () => {
        const result = await openCase(folder, name);
        if (options?.navigationOwner !== undefined && options.navigationOwner !== navigationSerial.current) return;
        if (options?.expectedIdentity && result.case?.identity !== options.expectedIdentity) {
          setCaseNotice({ state: "failed", reason: "The previous capture changed or is unavailable. The current work is kept." });
          return;
        }
        if (result.case) {
          clearCase();
          setSourceKind("case");
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
      focusRegion("evidence", true);
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
      catalogPath?: string,
      selection?: HL7ReferenceSelection,
      catalogIdentity?: string,
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
      if (catalogPath !== undefined) {
        if (catalogPath !== referenceCatalog.current) referenceIdentity.current = catalogIdentity ?? "";
        referenceCatalog.current = catalogPath;
      }
      if (catalogIdentity !== undefined) referenceIdentity.current = catalogIdentity;
      if (selection !== undefined) referenceSelection.current = selection;
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
          ...(referenceCatalog.current ? { reference_catalog: referenceCatalog.current } : {}),
          ...(referenceIdentity.current ? { reference_identity: referenceIdentity.current } : {}),
          ...(referenceSelection.current ? { reference_selection: referenceSelection.current } : {}),
        });
        answer = result;
        // A field that is not in the message leaves the message as it was.
        if (current() && (result.state === "completed" || moving || path === "")) {
          if (!referenceIdentity.current && result.inspection?.reference?.identity) referenceIdentity.current = result.inspection.reference.identity;
          setInspectionResult(result);
        }
      });
      return answer;
    },
    [evidence, messages, revealed, root, run, selectedOccurrence],
  );

  const openFieldValues = (selector: string) => {
    if (!root || !verified || verified.protocol === "fhir-r4") return;
    const at = inspectionResult?.inspection;
    const body = document.querySelector<HTMLElement>('.page[data-page="cases"] .page-body');
    setFieldValuesContext({ workspace: root, case: verified.name, identity: verified.identity, selector,
      ...(at?.metadata.label ? { label: at.metadata.label } : {}),
      occurrence: selectedOccurrence, path: at?.selected.path ?? "", nodeOffset: at?.node_offset ?? 0,
      byteOffset: at?.byte_offset ?? -1, scrollTop: body?.scrollTop ?? 0 });
    setFieldValuesInspecting(false);
    setRevealed(false);
    setCaseFlow("field-values");
  };
  const leaveFieldValues = () => {
    const origin = fieldValuesContext;
    setFieldValuesInspecting(false);
    setCaseFlow(null);
    setFieldValuesContext(null);
    setRevealed(false);
    if (!origin || root !== origin.workspace || verified?.name !== origin.case || verified.identity !== origin.identity) return;
    restoredScroll.current = origin.scrollTop;
    if (origin.occurrence) void inspect(origin.occurrence, origin.path, origin.nodeOffset, origin.byteOffset,
      { case: origin.case, identity: origin.identity }, false);
    else { setSelectedOccurrence(null); setInspectionResult(null); }
  };

  const openRequirements = (occurrence: string, selector: string, flow:"requirements"|"value-maps"="requirements") => {
    if (!root || !verified || verified.protocol === "fhir-r4" || !selectedSourceRef) return;
    const at = inspectionResult?.inspection;
    const body = document.querySelector<HTMLElement>('.page[data-page="cases"] .page-body');
    setRequirementsContext({ workspace: root, case: verified.name, identity: verified.identity, source: selectedSourceRef,
      occurrence, selector, edition:at?.metadata.hl7_version??"", originOccurrence: selectedOccurrence, originPath: at?.selected.path ?? "",
      nodeOffset: at?.node_offset ?? 0, byteOffset: at?.byte_offset ?? -1, scrollTop: body?.scrollTop ?? 0, view: caseView });
    setRevealed(false);
    setCaseFlow(flow);
  };
  const leaveRequirements = () => {
    const origin = requirementsContext;
    setCaseFlow(null);
    setRequirementsContext(null);
    setRevealed(false);
    if (!origin || root !== origin.workspace || verified?.name !== origin.case || verified.identity !== origin.identity) return;
    restoredScroll.current = origin.scrollTop;
    if (origin.view !== "findings" && origin.originOccurrence) void inspect(origin.originOccurrence, origin.originPath,
      origin.nodeOffset, origin.byteOffset, { case: origin.case, identity: origin.identity }, false);
  };

  // One practice run of the demo against its defective or fixed receiver. It
  // sends only on loopback; what the project then holds is read back, and the
  // run opens on its ordinary page.
  const practise = useCallback(
    async (trial: "baseline" | "post-fix", output: string) => {
      if (!root || !demo?.spec) return;
      const spec = demo.spec;
      let reason: string | null = null;
      await run("practice", async () => {
        const answer = await runPractice({ workspace: root, spec, trial, output });
        if (answer.state !== "completed") reason = answer.reason ?? "The run did not complete.";
      });
      setDemoNotice(reason);
      const answer = await demoProgress({ project: root, generation: 0 });
      if (answer.state !== "completed" || !answer.demo) return;
      setDemo(answer.demo);
      const ref = answer.demo.steps.find((step) => step.id === (trial === "baseline" ? "run-defective" : "run-fixed"))?.ref;
      if (ref) openRunPage(ref.id);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [demo, root, run],
  );

  // Run on a suite opens the reviewed send of that exact saved version at the
  // environment chosen; its Send is the only thing that sends.
  const handoff = useCallback((selection: SuiteRunHandoff) => {
    setSendRequest({ kind: "suite", suite: selection.target.suite, environment: selection.environment,connectedRequired:selection.connected===true });
  }, []);

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
      if((draft.kind==="interface-association"&&draft.content_schema==="readmit-interface-association-editor/v1") || (draft.kind==="capture-source-context"&&draft.content_schema==="readmit-capture-source-context-editor/v1")) {
        const content=draft.content as import("./bindings").InterfaceAssociationEditorDraft|import("./bindings").CaptureSourceEditorDraft;
        const owner=navigationSerial.current;const request=new RequestScope().enter(draft.workspace,content.project_id);
        const listing=await listWholeCatalog({context:request,kind:content.source.kind,filter:{}});
        if(navigationSerial.current!==owner || currentRoute.current.projectId!==draft.workspace)return;
        const item=listing.page?.items.find(item=>item.ref.id===content.source.id);const entry=item?.summary.case?.entry??item?.summary.variant?.entry;
        if(!entry){setResumeNotice("The original source is unavailable. Authored context work stays retained without rebinding.");return;}
        const opened=await verifyCase(draft.workspace,entry,{navigationOwner:owner,expectedIdentity:content.identity,skipAutoGrid:true});
        if(!opened?.case){setResumeNotice("The original source changed. Authored context work stays retained without adopting replacement evidence.");return;}
        if(draft.kind==="capture-source-context")setCaptureContextSource(content.source);
        else if("occurrence" in content){setRequirementsContext({workspace:draft.workspace,case:entry,identity:content.identity,source:content.source,occurrence:content.occurrence,selector:content.selector,edition:"",originOccurrence:content.occurrence,originPath:content.selector,nodeOffset:0,byteOffset:-1,scrollTop:0,view:"messages"});setRevealed(false);setCaseFlow("requirements");}
        return;
      }
      if (draft.kind === "field-value-map" && draft.content_schema === "readmit-field-value-map-editor/v1") {
        const content = draft.content as ValueMapEditorDraft;
        const owner = navigationSerial.current;
        const context = new RequestScope().enter(draft.workspace);
        const listed = await listWholeCatalog({context,kind:content.source.kind,filter:{}});
        if (navigationSerial.current !== owner || currentRoute.current.projectId !== draft.workspace) return;
        const item=listed.page?.items.find(candidate=>candidate.ref.id===content.source.id);
        const entry=item?.summary.case?.entry??item?.summary.variant?.entry;
        if(!entry){setUnavailableMapDraft({draft,reason:"The original source is missing or unavailable. Authored map work is kept without repinning to another source."});return;}
        const opened=await verifyCase(draft.workspace,entry,{navigationOwner:owner,expectedIdentity:content.identity,skipAutoGrid:true});
        if(!opened?.case){setUnavailableMapDraft({draft,reason:"The original source changed or cannot be verified. Authored map work is kept; contextual inspection is unavailable."});return;}
        setRequirementsContext({workspace:draft.workspace,case:entry,identity:content.identity,source:content.source,occurrence:content.occurrence,selector:content.selector,edition:content.edition,originOccurrence:content.occurrence,originPath:content.selector,nodeOffset:0,byteOffset:-1,scrollTop:0,view:"messages"});
        setRevealed(false);setCaseFlow("value-maps");setResumeNotice(null);return;
      }
      if (SHEET_DRAFTS.has(draft.kind)) {
        setResumeNotice(null);
        setResuming(draft);
        return;
      }
      if (draft.kind === "test-draft" && (draft.content_schema === TEST_EDITOR_DRAFT || draft.content_schema === CONNECTED_EDITOR_DRAFT || draft.content_schema === EXCHANGE_EDITOR_DRAFT)) {
        setResumeNotice(null);
        setRestoringTest(draft);
        return;
      }
      if (draft.kind === "suite-editor" && (draft.content_schema === SUITE_EDITOR_DRAFT || draft.content_schema === CONNECTED_SUITE_EDITOR_DRAFT)) {
        setResumeNotice(null);
        setRestoringSuite(draft);
        return;
      }
      const scenarioObject = scenarioDraftObject(draft);
      if (scenarioObject) {
        setResumeNotice(null);
        routeTo({ type: "go", to: { destination: "library", view: "scenarios", objectId: scenarioObject } });
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
      if (place.route) {
        // A share draft continues the share of the report it is of.
        const report = draft.kind === SHARE_DRAFT_KIND ? (draft.item?.ref.id ?? (draft.content as { report?: string }).report) : undefined;
        routeTo({ type: "go", to: report ? { ...place.route, objectId: report } : place.route });
      }
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

  // A save that wrote a new workspace entry changes what the folder lists, so
  // the listing is read again and the pickers offer the new revision. The open
  // case and everything derived from it stay exactly as they are.
  // Every read of the open project carries its request context, so an answer
  // that arrives after the person moved to another project is dropped.
  const projectScope = useRef(new RequestScope());
  const projectContext = useCallback(() => projectScope.current.enter(root ?? ""), [root]);
  const exchangeScope = useRef(new RequestScope());
  const exchangeContext = useCallback(() => exchangeScope.current.enter(root ?? ""), [root]);
  const exportScope = useRef(new RequestScope());
  const exportContext = useCallback(() => exportScope.current.enter(root ?? ""), [root]);

  const casesScope = useRef(new RequestScope());
  const refreshCases = useCallback(async () => {
    const context = casesScope.current.enter(root ?? "");
    setCasesFailure(null);
    if (!root) {
      setCases([]);
      setCasesLoading(false);
      return;
    }
    setCasesLoading(true);
    const listed = await listWholeCatalog({ context, kind: "case", filter: {} });
    // A refused admission may have no reply context. Ownership is the request
    // we issued; unrelated project reads must not invalidate this list.
    if (!casesScope.current.current({ context })) return;
    setCasesLoading(false);
    if (listed.state !== "completed" && listed.state !== "empty") {
      setCasesFailure(listed.reason ?? "The cases could not be read.");
      return;
    }
    setCases(listed.page?.items ?? []);
    listedCases.current = listed.page?.items ?? [];
  }, [root]);

  // The project object one case entry is: a variant, or a case, with its
  // name and, for a variant, the case it was made from.
  const objectScope = useRef(new RequestScope());
  const sourceProjectId = projects.find((project) => project.summary.project?.folder === root)?.ref.id ?? "";
  const objectAt = useCallback(
    async (entry: string, scope = objectScope.current): Promise<{ ref: ItemRef; name: string; parent: ItemRef | null } | null> => {
      const context = scope.enter(root ?? "", sourceProjectId);
      const [variants, listed] = await Promise.all([
        listWholeCatalog({ context, kind: "variant", filter: {} }),
        listWholeCatalog({ context, kind: "case", filter: {} }),
      ]);
      // The facade fills an omitted project identity after resolving the folder.
      // Request ownership belongs to the issued context; a normalized reply must
      // still name this project and agree with its known immutable identity.
      const matchesProject = (reply: RequestContext) => reply.project === context.project && (!context.project_id || reply.project_id === context.project_id);
      if (currentRoute.current.projectId !== root || !scope.current({ context }) || !matchesProject(variants.context) || !matchesProject(listed.context)) return null;
      const variant = variants.page?.items.find((item) => item.summary.variant?.entry === entry);
      if (variant) return { ref: variant.ref, name: variant.name, parent: variant.summary.variant?.parent ?? null };
      const held = listed.page?.items.find((item) => item.summary.case?.entry === entry);
      return held ? { ref: held.ref, name: held.name, parent: null } : null;
    },
    [root, sourceProjectId],
  );
  useEffect(() => {
    let current = true;
    setExchangeSource(null);
    if (!root || !verified || verified.protocol === "fhir-r4") return;
    const source = verified;
    // Source association and the case's displayed object are independent read
    // owners; the latter must not invalidate this source's generation.
    const associationScope = new RequestScope();
    void objectAt(source.name, associationScope).then((object) => {
      if (current && object) setExchangeSource({ project: root, entry: source.name, identity: source.identity, ref: object.ref });
    });
    return () => { current = false; };
  }, [root, verified?.name, verified?.identity, verified?.protocol, objectAt]);
  // Create variant starts from the case open now and the messages chosen in
  // it; Compare from the case open now and, when named, the other case.
  const startVariant = useCallback(
    async (entry: string, identity: string, seed: string[], protocol?: string) => {
      const object = await objectAt(entry);
      if (!object) return;
      setVariantFlow((held) => ({ source: { ref: object.ref, name: object.name, entry, identity, ...(protocol ? { protocol } : {}) }, seed, serial: (held?.serial ?? 0) + 1 }));
      setCaseFlow("variant");
    },
    [objectAt],
  );
  const startCompare = useCallback(
    async (entry: string, identity: string, other: ItemRef | null, tab?: ComparisonTab) => {
      const object = await objectAt(entry);
      if (!object) return;
      setCompareFlow((held) => ({ current: { ref: object.ref, name: object.name, entry, identity }, other, tab, serial: (held?.serial ?? 0) + 1 }));
      setCaseFlow("compare");
    },
    [objectAt],
  );

  // A case task from a row's menu. Those that belong to the open case's own
  // pages open it there; the rest open their sheet.
  const caseAction = useCallback(
    (item: CatalogItem, action: CaseAction) => {
      const entry = caseEntry(item);
 if(action==="source-context") {setCaptureContextSource(item.ref);return;}
      if ((action === "variant" || action === "compare") && root && entry) {
        void verifyCase(root, entry).then((opened) => {
          if (!opened?.case) return;
          if (action === "variant") void startVariant(entry, opened.case.identity, [], opened.case.protocol);
          else void startCompare(entry, opened.case.identity, null);
        });
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
    [leaving, root, routeTo, startCompare, startVariant, verifyCase],
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
        if (!projectScope.current.current(answer)) return;
        setFiles(answer.files);
        setFilesFailure(answer.state === "completed" || answer.state === "empty" ? null : (answer.reason ?? "The project's files could not be listed."));
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
    setFilesFailure(null);
    setCaseNotices({});
    setAttachmentNotice(null);
    setResumeNotice(null);
    setCasesLoading(true);
    void refreshCases();
    return () => { casesScope.current.next(); };
  }, [refreshCases]);

  // A retained sheet draft reopens its sheet once its project is open and its
  // object is listed. One whose object is gone stays among the drafts.
  useEffect(() => {
    if (!resuming || root !== resuming.workspace) return;
    const ref = resuming.item?.ref;
    // A project note needs only its verified project. An unrelated case-list
    // read may still be in flight, and must not strand its recovery.
    if (casesLoading && !(resuming.kind === "note" && ref?.kind === "project")) return;
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
    async (ref: ItemRef, selection?: import("./bindings").RetainedImportSelection) => {
      await leaveImport();
      await refreshCases();
      const entry = listedCases.current.find((item) => item.ref.id === ref.id)?.summary.case?.entry;
      if (root && entry && currentRoute.current.projectId===root) {
 const opened=await verifyCase(root,entry,selection ? {skipAutoGrid:true,expectedIdentity:selection.identity} : undefined);
 if(selection && opened?.case && currentRoute.current.projectId===root) {
  await loadMessages(root,{case:entry,identity:selection.identity},NO_QUERY,null,0);
  setCheckedMessages(new Set(selection.messages));
  if(selection.selected) await inspect(selection.selected,selection.path??"",selection.node_offset??0,-1,{case:entry,identity:selection.identity},false);
 }
}
    },
    [leaveImport, refreshCases, root, verifyCase, loadMessages,inspect],
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

  // Drops one retained editor draft and takes it out of the local list at the
  // same moment, so a panel cannot offer the same draft back again while the
  // facade's answer is still in flight.
  const dropDraft = useCallback((id: string) => {
    setDrafts((current) => current?.filter((draft) => draft.id !== id) ?? null);
    void discardEditorDraft(id);
  }, []);

  // Reopening a private session verifies its sources before restoring local
  // navigation. Unstored work remains in the existing draft store; external
  // execution and consent are never resumed by that restoration.
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
      navigationSerial.current += 1;
      const exit = () => {
        if (to === "messages") {
          routeTo({ type: "go", to: sourceKind === "case" && evidence?.case ? { destination: "cases", objectId: evidence.case.name, view: "messages" } : fileReader.navigation?.file ? { destination: "inspect-file" } : { destination: "messages" }, leaving: leaving() });
        } else if (to === "cases") {
          routeTo({ type: "go", to: { destination: "cases" }, leaving: leaving() });
        } else routeTo({ type: "destination", destination: to, leaving: leaving() });
        focusRegion("evidence");
      };
      if (scenarioNavigation.current) scenarioNavigation.current(exit);
      else exit();
    },
    [evidence?.case, sourceKind, fileReader.navigation?.file, focusRegion, leaving, routeTo],
  );

  useEffect(()=>{
 if(root && looseRetention && retentionStarted.current!==root) {retentionStarted.current=root;go("cases");setImporting(true);}
 },[root,looseRetention,go]);

  // Opens a place reached from inside another: a child destination, or a
  // destination at one of its views.
  const open = useCallback(
    (to: Route) => {
      navigationSerial.current += 1;
      const exit = () => {
        routeTo({ type: "go", to, leaving: leaving() });
        focusRegion("evidence");
      };
      if (scenarioNavigation.current) scenarioNavigation.current(exit);
      else exit();
    },
    [focusRegion, leaving, routeTo],
  );

  const back = useCallback(() => {
    navigationSerial.current += 1;
    const exit = () => {
      routeTo({ type: "back" });
      focusRegion("evidence");
    };
    if (scenarioNavigation.current) scenarioNavigation.current(exit);
    else exit();
  }, [focusRegion, routeTo]);

  const configureTargetFromSource=()=>{
 if(!root)return;
 const from=currentRoute.current;
 open({destination:"environments",view:"add",origin:{projectId:root,destination:from.destination,...(from.objectId ? {objectId:from.objectId}:{}),...(from.view ? {view:from.view}:{}),returnContext:leaving(selectedOccurrence??undefined),...(verified ? {evidence:{entry:verified.name,identity:verified.identity}}:{})}});
 };

  useEffect(()=>{if(reviewSetup && reviewSetup.project!==root)setReviewSetup(null);},[root,reviewSetup]);

  const [returnNotice, setReturnNotice] = useState<string | null>(null);
  const returnFromSetup = useCallback(async () => {
    const setup = currentRoute.current;
    const origin = setup.origin;
    if (!origin || !root || origin.projectId !== root || setup.projectId !== root) {
      setReturnNotice("The originating project is no longer open. Your current work is kept.");
      return;
    }
    setReturnNotice(null);
    let evidence: NavigationEvidence | undefined;
    if (origin.evidence) {
      const answer = await openCase(root, origin.evidence.entry);
      // Another navigation or project switch owns the window now.
      if (currentRoute.current !== setup) return;
      if (answer.state !== "completed" || !answer.case || answer.case.identity !== origin.evidence.identity) {
        setReturnNotice(origin.destination==="new-test" || origin.destination==="edit-test" ? "The originating evidence changed or is unavailable. Your test draft and current work are kept." : "The originating evidence changed or is unavailable. Your originating work is kept.");
        return;
      }
      evidence = { entry: origin.evidence.entry, identity: answer.case.identity };
    }
    routeTo({ type: "return", ...(evidence ? { evidence } : {}) });
 if(reviewSetup?.project===root) {setSendRequest(reviewSetup.request);setReviewSetup(null);}
    focusRegion("evidence");
  }, [root, routeTo, focusRegion,reviewSetup]);

  // Environments reads under its own request scope, so its reads never make
  // another list's answer look stale.
  const environmentScope = useRef(new RequestScope());
  const environmentContext = useCallback(() => environmentScope.current.enter(root ?? ""), [root]);
  // Tests reads under its own request scope. A saved test, a new test and an
  // edit are each their own place, with Back to where they were opened.
  const testRoute = route.origin && (route.origin.destination === "new-test" || route.origin.destination === "edit-test") ? route.origin : route;
  const testsPlace: TestsPlace =
    testRoute.destination === "new-test"
      ? { kind: "new" }
      : testRoute.destination === "edit-test" && testRoute.objectId
        ? { kind: "edit", id: testRoute.objectId }
        : place === "tests" && route.objectId
          ? { kind: "test", id: route.objectId, view: isTestWorkspaceView(route.view) ? route.view : "setup" }
          : { kind: "list" };
  const openPromotedExchange = async (provenance: ExchangeTestProvenance) => {
    if (!root) return;
    const project = root;
    const owner = navigationSerial.current;
    const source = provenance.inputs;
    const request = projectContext();
    const listing = await listWholeCatalog({ context: request, kind: source.case.kind, filter: {} });
    if (navigationSerial.current !== owner || currentRoute.current.projectId !== project) return;
    const item = listing.page?.items.find((candidate) => candidate.ref.id === source.case.id);
    const entry = item?.summary.variant?.entry ?? item?.summary.case?.entry;
    if (!entry) { setReturnNotice("The retained exchange source is unavailable. The draft is kept."); return; }
    const opened = await verifyCase(project, entry, { navigationOwner: owner, expectedIdentity: source.identity });
    if (!opened?.case) { setReturnNotice("The retained exchange source changed or is unavailable. The draft is kept."); return; }
    setRequestedExchange({ project, origin: provenance.origin });
  };

  const openSelectedTestWork=(destination:"runs"|"reports",objectId?:string)=>{
    const from=currentRoute.current;
    open({destination,...(objectId ? {objectId}:{}),...(root ? {origin:{projectId:root,destination:from.destination,...(from.objectId ? {objectId:from.objectId}:{}),...(from.view ? {view:from.view}:{}),returnContext:leaving()}}:{})});
  };
  const tests = useTests({
    onOpenRun:run=>openSelectedTestWork("runs",run.id),
    onOpenSuite:suite=>open({destination:"suite",objectId:suite.id,view:"tests"}),
    onOpenCapture:ref=>void openObjectRef(ref),
    onOpenReport:report=>openSelectedTestWork("reports",report.id),
    onCreateReport:(run,comparison)=>{setReportSeed(held=>({run,...(comparison ? {comparison}:{}),count:(held?.count??0)+1}));openSelectedTestWork("reports");},
    root,
 projectId:currentProject?.ref.id??"",
 retainedDrafts:drafts??[],
 onResumeDraft:draft=>void resumeDraft(draft),
    onOpenExchange: (provenance) => { void openPromotedExchange(provenance); },
    onTargetSetup: (evidence, selection) => {
      if (!root || currentRoute.current.projectId !== root) return;
      const from = currentRoute.current;
      setReturnNotice(null);
      open({ destination: "environments", view: "add", origin: {
        projectId: root, destination: from.destination,
        ...(from.objectId ? { objectId: from.objectId } : {}),
        ...(from.view ? { view: from.view } : {}),
        returnContext: leaving(selection), evidence,
      } });
    },
    addCheckGroup: pendingCheckGroup,
    onCheckGroupAdded: () => setPendingCheckGroup(null),
    restoreDraft: restoringTest,
    seedTest,
    onSeeded: (reason) => {
      setSeedTest(null);
      if (reason) setDemoNotice(reason);
    },
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
    onRun: (test) => setSendRequest({ kind: "test", test }),
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
    onSchedule: (suite) => openReusable("schedules",undefined,`suite:${suite}`),
    onTestLibrary: category=>tests.browseTests(category),
    restoreDraft: restoringSuite,
    onRestored: (reason) => {
      setRestoringSuite(null);
      if (reason) setResumeNotice(reason);
    },
  });

  // Runs (#555): the history, the run this window is sending, one run's page
  // and two runs compared. A send is started only by a review's Send.
  const runsScope = useRef(new RequestScope());
  const runsContext = useCallback(() => runsScope.current.enter(root ?? ""), [root]);
  const runActivity = useRunActivity(root, (ended) => {
    setRunsEnded((count) => count + 1);
    const here = currentRoute.current;
    if (here.destination === "runs" && here.objectId === "active") routeTo({ type: "replace", to: { destination: "runs", objectId: ended.id } });
  });
  const runsPlace: RunsPlace =
    place === "runs" && route.objectId
      ? route.objectId === "active"
        ? { kind: "active" }
        : { kind: "run", id: route.objectId.split(":")[0]!, job: route.objectId.split(":")[1], view: route.view ?? "" }
      : { kind: "list" };
  const openRunPage = useCallback((id: string) => open({ destination: "runs", objectId: id }), [open]);
  const runsList = useRuns({
    root,
    shown: place === "runs" && runsPlace.kind === "list",
    busy,
    generation: runsEnded,
    onOpen: openRunPage,
    onRun: setSendRequest,
    onCompare: (ids) => open({ destination: "compare-runs", objectId: ids.join("..") }),
    onSchedules: () => open({ destination: "schedules" }),
    activity: runActivity.activity,
  });
  /** Opens a case's Messages at the occurrences a run or report names. */
  const viewMessages = (caseRef: ItemRef, occurrences: string[]) => {
    const entry = listedCases.current.find((item) => item.ref.id === caseRef.id)?.summary.case?.entry;
    if (!root || !entry) return;
    void verifyCase(root, entry, { skipAutoGrid: true }).then((opened) => {
      if (!opened?.case) return;
      setEvidenceFocus(occurrences.map((occurrence) => ({ occurrence, field: "" })));
      setEvidenceFrom("run");
      void loadMessages(root, { case: entry, identity: opened.case.identity }, NO_QUERY, null, 0, occurrences);
    });
  };
  const runPage = useRunPage({
    root: place === "runs" ? root : null,
    place: runsPlace,
    activity: runActivity.activity,
    busy,
    go: (to) => {
      if (to.kind !== "run") return;
      const objectId = to.job ? `${to.id}:${to.job}` : to.id;
      if (route.objectId === objectId) routeTo({ type: "view", view: to.view });
      else open({ destination: "runs", objectId, view: to.view });
    },
    onStop: runActivity.stop,
    onRun: setSendRequest,
    onCreateReport: () => {
      if (runsPlace.kind !== "run") return;
      const run = { kind: "run" as const, id: runsPlace.id };
      const job = runsPlace.job;
      setReportSeed((held) => ({ run, job, count: (held?.count ?? 0) + 1 }));
      open({ destination: "reports" });
    },
    onViewMessages: (caseRef, occurrences) => viewMessages(caseRef, occurrences),
    onOpenObservation: (observation) => open({ destination: "environments", objectId: `observation:${observation.id}` }),
    onRead: (answer) => {
      const defective = demo?.steps.find((step) => step.id === "run-defective")?.ref?.id;
      if (defective && answer.state === "completed" && answer.run?.item.ref.id === defective && answer.run.checks.some((check) => check.result === "failed")) seeDemo("view-failed-check");
    },
    onMinimize: (run) => open({ destination: "minimize-failure", objectId: run.id }),
  });
  // Minimize failure (#558): the series this window runs keeps running while
  // the person is elsewhere.
  const minimizeActivity = useMinimizeActivity(root);
  const minimize = useMinimize({
    root: place === "minimize-failure" ? root : null,
    context: runsContext,
    run: place === "minimize-failure" && route.objectId ? { kind: "run", id: route.objectId } : null,
    busy,
    activity: minimizeActivity.activity,
    onStart: (run, review, intent, execution) => {
      minimizeActivity.start(run, review, intent, execution);
      void execution.then(() => setRunsEnded((count) => count + 1));
    },
    onStop: minimizeActivity.stop,
    onOpenRun: (run) => open({ destination: "runs", objectId: run.id }),
    onOpenVariant: (variant) => {
      go("cases");
      void openObjectRef(variant);
    },
    onEditEnvironment: (environment) => open({ destination: "environments", objectId: environment.id }),
  });
  const runComparison = useRunComparison({
    root: place === "compare-runs" ? root : null,
    runs: place === "compare-runs" ? (route.objectId ?? "").split("..").filter(Boolean) : [],
    view: route.view ?? "",
    onView: setView,
    onRuns: (ids) => routeTo({ type: "replace", to: { destination: "compare-runs", objectId: ids.join("..") } }),
    onOpen: openRunPage,
    onCompared: (runs, answer) => {
      const pair = ["run-defective", "run-fixed"].map((key) => demo?.steps.find((step) => step.id === key)?.ref?.id);
      if (answer.state === "completed" && pair.every((id) => id && runs.includes(id))) seeDemo("compare");
    },
  });

  // Try demo opens the synthetic demo project, creating it the first time in
  // the application's own storage; a project of the person's is never touched.
  function tryDemo() {
    void openFolder(async () => {
      const opened = await openDemoProject();
      if (opened.state !== "completed") return { state: opened.state, ...(opened.reason ? { reason: opened.reason } : {}) };
      return openWorkspace(opened.context.project);
    });
  }

  // One step of the demo: each opens its ordinary screen, and a step is done
  // only from that screen's actual result.
  const seeDemo = (id: DemoStepID) => setDemoSeen((held) => (held.has(id) ? held : new Set([...held, id])));
  const demoStep = (id: DemoStepID) => {
    if (!root || !demo) return;
    const step = demo.steps.find((entry) => entry.id === id);
    setDemoNotice(null);
    switch (id) {
      case "open-messages":
        void verifyCase(root, demo.case).then((opened) => {
          if (opened?.case) seeDemo("open-messages");
        });
        return;
      case "create-test":
        if (demo.case_ref) setSeedTest({ case: demo.case_ref, test: demo.test });
        return;
      case "run-defective":
      case "run-fixed":
        if (step?.output) void practise(id === "run-defective" ? "baseline" : "post-fix", step.output);
        return;
      case "view-failed-check": {
        const ref = demo.steps.find((entry) => entry.id === "run-defective")?.ref;
        if (ref) open({ destination: "runs", objectId: ref.id, view: "checks" });
        return;
      }
      case "compare": {
        const runs = ["run-defective", "run-fixed"].map((key) => demo.steps.find((entry) => entry.id === key)?.ref?.id);
        if (runs.every(Boolean)) open({ destination: "compare-runs", objectId: runs.join("..") });
        return;
      }
    }
  };
  // The demo's project steps are read again as the person moves, so a test
  // created in New test is done once the project holds it.
  useEffect(() => {
    if (demo && root) void refreshDemo(root);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [place, route.objectId]);

  const benchmarks = useBenchmarks({ root, shown: place === "benchmarks", busy });
  // Schedules (#564): the project's one collection, narrowed to the suite or
  // runner it was opened from.
  const scheduleFilter = place === "schedules" ? (route.objectId ?? "") : "";
  const schedules = useSchedules({
    root,
    shown: place === "schedules",
    ...(scheduleFilter.startsWith("suite:") ? { suite: scheduleFilter.slice("suite:".length) } : {}),
    ...(scheduleFilter.startsWith("runner:") ? { runner: scheduleFilter.slice("runner:".length) } : {}),
  });
  const helpArticleId = place === "help-article" ? route.objectId : undefined;
  const openHelpArticle = (id: string) => open({ destination: "help-article", objectId: id });
  const helpAction = (action: HelpActionID) => {
    // A task needs a project; without one, Projects is where one is chosen.
    if (!root) {
      go("home");
      return;
    }
    switch (action) {
      case "start-import":
        go("cases");
        setImporting(true);
        return;
      case "open-cases":
        go("cases");
        return;
      case "new-test":
        open({ destination: "new-test" });
        return;
      case "add-environment":
        open({ destination: "environments", view: "add" });
        return;
      case "open-reports":
        go("reports");
        return;
    }
  };
  const helpArticle = useHelpArticle(helpArticleId, helpAction);
  // Reports reads under its own request scope: its list, and one report.
  const reportsPlace: ReportsPlace = place === "reports" && route.objectId ? { kind: "report", id: route.objectId } : { kind: "list" };
  const openRunFrom = (run: ItemRef, job?: string) => open({ destination: "runs", objectId: job ? `${run.id}:${job}` : run.id });
  const reports = useReports({
    root,
    shown: place === "reports",
    place: reportsPlace,
    go: (to) => open({destination:"reports",...(to.kind==="report" ? {objectId:to.id}:{}),...(route.origin ? {origin:route.origin}:{})}),
    busy,
    seed: reportSeed,
  });
  const reportPage = useReportPage({
    root: place === "reports" ? root : null,
    id: reportsPlace.kind === "report" ? reportsPlace.id : null,
    busy,
    listed: reports.listed,
    onOpenRun: openRunFrom,
    onViewMessages: viewMessages,
    onShare: (start) => (reportsPlace.kind === "report" ? open({ destination: "share-report", objectId: reportsPlace.id, view: start }) : undefined),
    onSupport: () => (reportsPlace.kind === "report" ? setSupportSheet({ report: { kind: "report", id: reportsPlace.id } }) : undefined),
    onChanged: () => void reports.refresh(),
  });
  // Share report (#560): one started flow over one report version, from its
  // Contents or, for Export, straight to its Preview.
  const share = useShareReport({
    root,
    projectId: currentProject?.ref.id ?? "",
    report: place === "share-report" && route.objectId ? { kind: "report", id: route.objectId } : null,
    start: (route.view === "preview" ? "preview" : "contents") as ShareStep,
    drafts,
    busy,
    onRun: setSendRequest,
    onManageTemplates: () => open({ destination: "share-templates" }),
    onLeave: back,
  });
  const packages = useEncryptedPackages({ root: place === "encrypted-packages" ? root : null, projectId: currentProject?.ref.id ?? "", shown: place === "encrypted-packages" });
  const shareTemplates = useShareTemplates({ root: place === "share-templates" ? root : null, projectId: currentProject?.ref.id ?? "", shown: place === "share-templates" });

  /** Create test: the case open now and its chosen messages (all of them
   * when none is chosen) go to the one test editor. */
  // The project object the open case is, read again whenever another case
  // opens.
  useEffect(() => {
    setOpenObject(null);
    if (!root || !verified) return;
    let live = true;
    void objectAt(verified.name).then((object) => {
      if (live && object) setOpenObject({...object,project:root,entry:verified.name,identity:verified.identity});
    });
    return () => {
      live = false;
    };
  }, [objectAt, root, verified]);
  // Opens one case or variant of the project by its reference: a saved
  // variant opens on its messages, the original case of a variant likewise.
  const openObjectRef = async (ref: ItemRef) => {
    if (!root) return;
    const [variants, listed] = await Promise.all([
      listWholeCatalog({ context: projectContext(), kind: "variant", filter: {} }),
      listWholeCatalog({ context: projectContext(), kind: "case", filter: {} }),
    ]);
    const item = [...(variants.page?.items ?? []), ...(listed.page?.items ?? [])].find((held) => held.ref.id === ref.id);
    const entry = item?.summary.variant?.entry ?? item?.summary.case?.entry;
    if (!entry) return;
    void refreshCases();
    await verifyCase(root, entry);
  };

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
    const chosen = [...checkedMessages];
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
    ...(verified?.protocol !== "fhir-r4" && selectedSourceRef ? { onRequirements: (finding: import("./bindings").FindingRow) => {
      const at = finding.evidence.find((entry) => entry.occurrence !== "");
      openRequirements(at?.occurrence ?? "", at?.field ?? "");
    } } : {}),
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
  const [viewingPacks, setViewingPacks] = useState(false);
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
  const importedLibraryDraft = (answer: ItemDraftResult) => {
    setLibraryImport(answer);
    openLibraryObject("import");
  };
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
    onImported: importedLibraryDraft,
    context: libraryContext,
    ref: libraryRef("profile"),
    imported: libraryKind === "profile" ? libraryImported : null,
    shown: place === "library" && libraryView === "profiles" && libraryObject !== undefined,
    busy,
    onSaved: librarySaved,
  });
  const scenarioPage = useScenario({
    root,
    object: libraryObject,
    drafts,
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
  scenarioNavigation.current = place === "library" && libraryView === "scenarios" && libraryObject !== undefined ? scenarioPage.leave : null;
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

  const newFHIRProfile = async () => {
    const context = libraryContext();
    const answer = await openItemDraft({ context, ref: { kind: "profile", id: "" }, protocol: "fhir-r4" });
    if (!libraryScope.current.current(answer)) return;
    if (answer.state !== "completed" || !answer.draft) {
      setLibraryNotice(answer.reason ?? "The profile editor could not be opened.");
      return;
    }
    setLibraryImport(answer);
    open({ destination: "library", view: "profiles", objectId: "import" });
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

  const environmentPage = useEnvironments({
    shown:place==="environments",
    root,
    context: environmentContext,
    place: environmentPlace(place === "environments" ? route.objectId : undefined, route.view),
    go: (objectId, view) => open({ destination: "environments", objectId, ...(view ? { view } : {}) }),
    back: route.origin ? () => void returnFromSetup() : back,
    busy,
    onAdded: (ref) => currentRoute.current.origin ? void returnFromSetup() : returnToSecurity(ref),
    capture: captureBinding,
    onChooseCapture: async (item) => {
      if(!root)return;
      const project=root;const from=currentRoute.current;const owner=navigationSerial.current;
      const entry=caseEntry(item);
      if(!entry || item.availability!=="available") {setReturnNotice("The selected capture is unavailable; the target context is kept.");return;}
      const origin={projectId:project,destination:from.destination,...(from.objectId ? {objectId:from.objectId}:{}),...(from.view ? {view:from.view}:{}),returnContext:leaving()};
      const opened=await verifyCase(project,entry,{navigationOwner:owner});
      if(!opened?.case || currentRoute.current.projectId!==project || currentRoute.current.destination!=="cases" || currentRoute.current.objectId!==entry)return;
      routeTo({type:"replace",to:{destination:"cases",objectId:entry,view:"messages",origin}});
    },
    onObservationClosed: () => {
      setCaptureBinding(null);
      if (observingFromSecurity) {
        setObservingFromSecurity(false);
        routeTo({ type: "replace", to: { destination: "environments" } });
        returnToSecurity();
      } else if (currentRoute.current.destination === "environments" && currentRoute.current.view === "add-observation" && currentRoute.current.objectId === undefined) {
        routeTo({ type: "replace", to: { destination: "environments" } });
        focusRegion("evidence");
      } else back();
    },
    onObservationSaved: (id) => {
      setCaptureBinding(null);
      if (observingFromSecurity) {
        setObservingFromSecurity(false);
        routeTo({ type: "replace", to: { destination: "environments", objectId: `observation:${id}` } });
        returnToSecurity(`observation:${id}`);
      } else open({ destination: "environments", objectId: `observation:${id}` });
    },
  });

  const environments = route.origin ? {
    ...environmentPage,
    actions: <><button type="button" disabled={busy} onClick={() => void returnFromSetup()}>{reviewSetup ? "Return to review":route.origin?.destination==="new-test" || route.origin?.destination==="edit-test" ? "Return to test":"Return to messages"}</button>{environmentPage.actions}</>,
    body: <>{returnNotice ? <p role="alert" className="object-problem">{returnNotice}</p> : null}{environmentPage.body}</>,
  } : environmentPage;

  const encryption = useEncryption({ root: place === "encryption" ? root : null, busy, onChanged: () => void refreshListing() });

  // The shown test's or environment's own actions, for the palette.
  useOfferedActions(registerActions, place === "tests" && "palette" in tests ? tests.palette : null);
  useOfferedActions(registerActions, place === "environments" && "palette" in environments ? environments.palette : null);

  // Storage reads under its own request scope, so its reads never make
  // another list's answer look stale.
  const storageScope = useRef(new RequestScope());
  const storageContext = useCallback(() => storageScope.current.enter(root ?? ""), [root]);

  const openSettings = useCallback((view: SettingsView) => open({ destination: "settings", view }), [open]);
  const openReusable = (destination:"settings"|"schedules",view?:string,objectId?:string) => {
    const from=currentRoute.current;
    open({destination,...(view ? {view}:{}),...(objectId ? {objectId}:{}),...(root ? {origin:{projectId:root,destination:from.destination,...(from.objectId ? {objectId:from.objectId}:{}),...(from.view ? {view:from.view}:{}),returnContext:leaving()}}:{})});
  };
  const backFromReusable = () => {
    if(route.origin && root===route.origin.projectId) routeTo({type:"return"});
    else back();
  };


  const openStorage = useCallback(() => openSettings("storage"), [openSettings]);

  // A task refused for want of a license opens License with its activation
  // flow, and a license installed there returns to that task, which is then
  // built again and taken explicitly: nothing queued runs on its own.
  const [licenseRequest, setLicenseRequest] = useState(0);
  const licenseReturn = useRef<(() => void) | null>(null);
  const activateFrom = (reopen?: () => void) => {
    const from = sidebarOf(currentRoute.current.destination);
    licenseReturn.current = () => {
      routeTo({ type: "destination", destination: from });
      reopen?.();
    };
    setLicenseRequest((held) => held + 1);
    openSettings("license");
  };

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
        setSetupFromSecurity("runner");
        requestSetup("runner");
        openSettings("runners");
        return;
      case "portal":
        setSetupFromSecurity("portal");
        requestSetup("portal");
        open({ destination: "license-setup" });
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
    "create-sample-workspace": () => tryDemo(),
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
    "performance-corpus": () => open({ destination: "benchmarks" }),
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
  const projectName = overview?.title || (root ? folderName(root) : "");
  const caseTitle = verified ? (openObject?.project===root && openObject.entry===verified.name && openObject.identity===verified.identity ? openObject.name:"") || overview?.cases.find((entry) => entry.name === verified.name)?.title || verified.name : "";
  const subpage = capturing && root ? "capture" : null;
  // The open case's own menu, which the palette also lists while the case is
  // on screen. A variant also links the case it was made from and its
  // changes.
  const parentCase = openObject?.parent ?? null;
  const caseMenu: MenuItem[] = [
    ...(verified?.protocol === "fhir-r4" ? [] : [{ label: "Create test", onSelect: () => void createTest() }]),
    { label: "Create variant", onSelect: () => verified && void startVariant(verified.name, verified.identity, [], verified.protocol) },
    { label: "Compare", onSelect: () => verified && void startCompare(verified.name, verified.identity, null) },
    ...(parentCase
      ? [
          { label: "Original case", onSelect: () => void openObjectRef(parentCase), separated: true },
          { label: "Changes", onSelect: () => verified && void startCompare(verified.name, verified.identity, parentCase, "plan") },
        ]
      : []),
    { label: "File details", onSelect: () => setFileDetails(true), separated: true },
    { label: "Close case", onSelect: backToProject },
  ];
  const variantEditor = useVariantEditor({
    root,
    context: projectContext,
    flow: caseFlow === "variant" ? variantFlow : null,
    drafts,
    busy,
    onSaved: (saved) => void openObjectRef(saved),
    onOpenMessage: (occurrence) => variantFlow
      ? void inspect(occurrence, "", 0, -1, { case: variantFlow.source.entry, identity: variantFlow.source.identity })
      : undefined,
  });
  const caseComparison = useCaseComparison({
    root,
    context: projectContext,
    flow: caseFlow === "compare" ? compareFlow : null,
    busy,
    onCompareRuns: (ids) => open({ destination: "compare-runs", objectId: ids.join("..") }),
  });
  useOfferedActions(registerActions, place === "cases" && !captureCollection && verified !== null && subpage === null && caseFlow === null ? { object: caseTitle, items: caseMenu } : null);
  const inspecting = selectedOccurrence !== null || inspectionResult !== null || running === "inspect";
  // The rows chosen for Create test and Send selected: only message rows, so an
  // ACK or unparsed row is never counted as outbound; none chosen is all rows.
  const shownCase = verified ? { case: verified.name, identity: verified.identity } : { case: "", identity: "" };
  const selectedRow = messages?.rows.find((row) => row.id === selectedOccurrence) ?? null;
  const fileSelection = place === "inspect-file" && fileReader.details !== null;
  const benchmarkSelection = place === "benchmarks" && benchmarks.details !== null;
  const findingShown = place === "cases" && !captureCollection && verified !== null && subpage === null && caseView === "findings" && findings.selected !== null;
  const linkShown = place === "cases" && !captureCollection && verified !== null && subpage === null && caseView === "timeline" && timeline.selectedLink !== null;
  const detailsShown = caseFlow !== "requirements" && caseFlow !== "value-maps" && (
    (place === "cases" && !captureCollection && verified !== null && subpage === null && caseView !== "findings" && inspecting && !linkShown && (caseFlow !== "field-values" || fieldValuesInspecting)) || findingShown || linkShown || fileSelection || benchmarkSelection);
  const closeDetails = () => {
    if (caseFlow === "field-values") { setFieldValuesInspecting(false); return; }
    if (fileSelection) {
      fileReader.closeDetails();
    } else if (benchmarkSelection) {
      benchmarks.closeDetails();
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

  // The window's own width decides the sidebar: a labelled 14rem sidebar where
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
      onProjects={() => go("home")}
    />
  ) : null;

  // The inspector keeps the width chosen for this project, clamped to the room
  // there is now. When the list beside it would fall below its useful width,
  // the selection is shown on its own with the way back to the list.
  const readerLayout = detailsShown && (fileSelection || (place === "cases" && caseFlow !== "field-values" && caseView === "messages" && verified?.protocol !== "fhir-r4"));
  const compactReader = readerLayout && (readerDensityOverride ?? (frameWidth > 0 && frameWidth / rem <= READER_COMPACT_WINDOW_REM));
  const sidebarRem = compact ? ICON_RAIL_REM : compactReader ? READER_COMPACT_SIDEBAR_REM : SIDEBAR_REM;
  const workareaRem = frameWidth > 0 ? frameWidth / rem - sidebarRem : Number.POSITIVE_INFINITY;
  const preferredInspector = (root !== null ? inspectorWidths[root] : undefined) ?? INSPECTOR_REM;
  const inspectorRem = Math.max(INSPECTOR_MIN_REM, Math.min(preferredInspector, INSPECTOR_MAX_REM, workareaRem - LIST_MIN_REM - 1 / rem));
  const messageBrowserRem = Math.max(10, Math.min(messageBrowserWidths[root ?? "standalone-file"] ?? (compactReader ? MESSAGE_BROWSER_COMPACT_REM : MESSAGE_BROWSER_REM), 25));
  const readerMinRem = 37;
  const readerRem = Number.isFinite(workareaRem) ? Math.max(readerMinRem, workareaRem - messageBrowserRem - 1 / rem) : readerMinRem;
  const readerMaxRem = Number.isFinite(workareaRem) ? Math.max(readerMinRem, workareaRem - 10 - 1 / rem) : 80;
  const detailOnly = detailsShown && (readerLayout ? workareaRem < messageBrowserRem + readerMinRem + 1 / rem : workareaRem - inspectorRem - 1 / rem < LIST_MIN_REM);

  useEffect(() => {
    if (place === "inspect-file" && fileNavigation?.file) setSourceKind("file");
  }, [place, fileNavigation?.file]);

  useEffect(() => {
    if (!described || restoreStarted.current) return;
    restoreStarted.current = true;
    const restoreOwner=navigationSerial.current;
    void (async () => {
      const answer = await workingSession();
      // A person who already chose another location owns that navigation.
      if (restoreOwner!==navigationSerial.current || currentRoute.current.destination !== "home" || currentRoute.current.projectId) { setSessionReady(true); return; }
      if (answer.state !== "completed" || !answer.session) {
        if (answer.state !== "completed" && answer.state !== "empty") setSessionNotice(answer.reason ?? "The previous session could not be read. It remains intact.");
        setSessionReady(true); return;
      }
      const saved = answer.session.view;
      if (!saved.workspace && !saved.navigation?.file) { setSessionReady(true); return; }
      if (saved.workspace && !(await openFolder(() => openWorkspace(saved.workspace),restoreOwner))) { setSessionReady(true); return; }
      const expectedOwner=restoreOwner+(saved.workspace ? 1:0);
      if(navigationSerial.current!==expectedOwner){setSessionReady(true);return;}
      pendingSessionOwner.current=expectedOwner;
      setPendingSession(saved);
    })();
  }, [described]); // Restore once; later navigation belongs to the viewer.

  useEffect(() => {
    const saved = pendingSession;
    if (!saved || (saved.workspace && root !== saved.workspace)) return;
    setPendingSession(null);
    const owner = pendingSessionOwner.current;
    pendingSessionOwner.current=null;
    if(owner===null || owner!==navigationSerial.current){setSessionReady(true);return;}
    void (async () => {
      const nav = saved.navigation;
      const stillOwned = () => navigationSerial.current === owner;
      const abandon = () => { setSessionReady(true); };
      let sourceAvailable = true;
      if (nav?.project_identity && nav.project_identity !== currentProject?.ref.id) {
        setSessionNotice("The previous project identity changed. Choose its source explicitly; no navigation was rebound.");
        setSessionReady(true); return;
      }
      if (nav?.source_kind === "file" && nav.file) {
        sourceAvailable = await fileReader.restore(nav);
        if (!sourceAvailable) setSessionNotice("The previous message file changed or is unavailable. Open it explicitly; no selection was rebound.");
        if (!stillOwned()) { abandon(); return; }
        setSourceKind("file");
      } else if (saved.case && root) {
        const opened = await verifyCase(root, saved.case, { skipAutoGrid: true, navigationOwner: owner, ...(nav?.source_identity ? { expectedIdentity: nav.source_identity } : {}) });
        if (!stillOwned()) { abandon(); return; }
        sourceAvailable = !!opened?.case;
        if (opened?.case) {
          const available = await listViews(root);
          if (!stillOwned()) { abandon(); return; }
          setViews(available.views);
          const chosen = nav?.filter ? available.views.find((view) => view.name === nav.filter) : undefined;
          if (nav?.filter && !chosen) setSessionNotice("The previous saved view is unavailable. The source stays inspectable.");
          const query = chosen?.query ?? NO_QUERY;
          const sort: SortState | null = nav?.sort === "time-ascending" || nav?.sort === "time-descending" ? { column: "time", direction: nav.sort === "time-ascending" ? "ascending" : "descending" } : null;
          setMessageView(chosen?.name ?? ""); setMessageQuery(query); setMessageSort(sort);
          await loadMessages(root, { case: opened.case.name, identity: opened.case.identity }, query, sort, 0);
          if (!stillOwned()) { abandon(); return; }
          setCheckedMessages(new Set(nav?.checked_occurrences??[]));
          const catalog = nav?.reference_path ?? "";
          if (nav?.reference_path) {
            const reference = await readReferenceCatalog(nav.reference_path);
            if (!stillOwned()) { abandon(); return; }
            if (reference.reference?.identity !== nav.reference_identity) setSessionNotice("The previous reference catalog changed or is unavailable. Select it again explicitly.");
          }
          referenceCatalog.current = catalog;
          referenceIdentity.current = catalog ? nav?.reference_identity ?? "" : "";
          let selection = nav?.reference_selection;
          if (selection) {
            const checked = await readHL7ReferenceSelection(selection);
            if (!stillOwned()) { abandon(); return; }
            if (checked.state !== "completed" || checked.overlay?.status === "not_available") setSessionNotice("A previous profile or documentation file changed or is unavailable. Select it again explicitly; the source stays inspectable.");
            // Retain the saved pins even when unavailable; inspection reports the
            // refusal instead of silently binding replacement bytes.
          }
          referenceSelection.current = selection;
          if (nav?.occurrence) await inspect(nav.occurrence, nav.field_path ?? "", nav.node_offset ?? 0, -1, { case: opened.case.name, identity: opened.case.identity }, false, -1, catalog, selection, referenceIdentity.current);
        } else setSessionNotice("The previous capture changed or is unavailable. Choose a source explicitly; no selection was rebound.");
      }
      if (!stillOwned()) { abandon(); return; }
      if (sourceAvailable) {
        const to: Route = nav && isPlace(nav.destination) ? { destination: nav.destination, ...(nav.object ? { objectId: nav.object } : {}), ...(nav.local_view ? { view: nav.local_view } : {}), ...(nav.scroll_top !== undefined ? { returnContext: { scrollTop: nav.scroll_top } } : {}) } : { destination: "cases", ...(saved.case ? { objectId: saved.case, view: "messages" } : {}) };
        // A review is recreated only by a deliberate action, never from session consent.
        if (to.objectId === "active" && to.destination === "runs") delete to.objectId;
        routeTo({ type: "replace", to });
        restoredScroll.current = nav?.scroll_top ?? 0;
        if (saved.region === "navigation" || saved.region === "evidence" || saved.region === "inspector") focusRegion(saved.region, true);
      }
      setSessionReady(true);
    })();
  }, [pendingSession, root]); // staged after the real project has opened

  useLayoutEffect(() => {
    if (restoredScroll.current === null || !sessionReady) return;
    const body = document.querySelector<HTMLElement>(`.page[data-page="${route.destination}"] .page-body`);
    if (body) body.scrollTop = restoredScroll.current;
    restoredScroll.current = null;
  }, [sessionReady, route, caseFlow]);

  // Every region the facade declares has an element here, for the same reason
  // every command has an action: a region the window forgot is a type error.
  // ReactElement rather than ReactNode, because ReactNode admits null and would
  // accept a region entered as nothing. What a region then draws is beyond it.
  const content: Record<RegionId, ReactElement> = {
    navigation: (
      <>
        {!readerLayout && (!root || compact) ? <ul className="nav-list">
          <NavItem id="home" label="Projects" current={destination === "home"} onSelect={go} />
        </ul> : null}
        {readerLayout && !compact ? <div className="sidebar-switcher reader-source-switcher"><Menu className="project-switcher" label={`Source: ${root ? projectName : fileReader.title}`} trigger={<><span className="reader-switcher-caption"><strong>{root ? projectName : fileReader.title}</strong><span>{root ? "Project" : "Local files"}</span></span><img className="workbench-icon" src={chevronsAsset} alt="" /></>} items={[{label:"Projects",onSelect:()=>go("home")},{label:"Open project…",onSelect:()=>perform("open-workspace"),disabled:busy},{label:"New project…",onSelect:()=>perform("new-project"),disabled:busy},{label:"Tools",onSelect:()=>go("tools")},...(root ? [{label:"Project settings",onSelect:()=>perform("open-project")},{label:"Files",onSelect:()=>open({destination:"project-files"})}] : [])]}/></div> : null}
        {root && !readerLayout && !compact ? <div className="sidebar-switcher">{switcher}</div> : null}
        {!root && !readerLayout ? <ul className="nav-list"><NavItem id="messages" label="Messages" current={destination === "messages"} onSelect={go} /></ul> : null}
        {root || readerLayout ? (
          <ul className="nav-list">
            {PROJECT_DESTINATIONS.map((item) => (
              <NavItem key={item.id} id={item.id} label={item.label} current={destination === item.id} onSelect={go} />
            ))}
          </ul>
        ) : null}
        <ul className="nav-list nav-footer">
          {GLOBAL_DESTINATIONS.filter(item=>item.id!=="tools" || (!root && !readerLayout)).map((item) => (
            <NavItem key={item.id} id={item.id} label={item.label} current={destination === item.id} onSelect={go} />
          ))}
        </ul>
        {root && demo ? (
          <DemoTask
            progress={demo}
            seen={demoSeen}
            shown={demoShown}
            busy={busy}
            notice={demoNotice}
            onStep={demoStep}
            onClose={() => setDemoShown(false)}
            onShow={() => setDemoShown(true)}
          />
        ) : null}
        {benchmarks.activity ? (
          // A benchmark or generation runs as the named corpus operation; this
          // returns to it, and its Stop cancels exactly that operation.
          <OperationIndicator label={benchmarks.activity.label} onOpen={() => open({ destination: "benchmarks" })} onStop={benchmarks.activity.stop} />
        ) : busy ? (
          <OperationIndicator label={running ? PROGRESS[running] : "Working…"} onStop={interruptible ? () => cancel() : undefined} />
        ) : capture.recording && !(place === "cases" && subpage === "capture") ? (
          // A capture keeps recording while the person is elsewhere; this
          // returns to it, and its Stop finishes it.
          <OperationIndicator label="Recording" onOpen={() => { go("cases"); setCapturing(true); }} onStop={capture.finish} />
        ) : runActivity.running && runActivity.activity ? (
          // A run keeps sending while the person is elsewhere; this returns
          // to it, and its Stop asks the facade to stop exactly this send.
          <OperationIndicator
            label={`Running ${runActivity.activity.review?.name ?? sendTitle(runActivity.activity.request).toLowerCase()}`}
            onOpen={() => open({ destination: "runs", objectId: "active" })}
            onStop={runActivity.stop}
          />
        ) : minimizeActivity.running && minimizeActivity.activity ? (
          // A minimization keeps running while the person is elsewhere; this
          // returns to it, and its Stop stops exactly this series.
          <OperationIndicator
            label={`Minimizing ${minimizeActivity.activity.review.minimize?.test_name ?? "failure"}`}
            onOpen={() => open({ destination: "minimize-failure", objectId: minimizeActivity.activity!.run.id })}
            onStop={minimizeActivity.stop}
          />
        ) : null}
      </>
    ),
    evidence: (
      <>
        {sessionNotice ? <p role="alert" className="object-problem">{sessionNotice}</p> : null}
 {sessionWriteNotice ? <p role="alert" className="object-problem">{sessionWriteNotice}</p> : null}
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
            <button type="button" className="quiet" disabled={busy} onClick={tryDemo}>
              Try demo
            </button>
          </div>
        </Page>

        <Page
          id="cases"
          shown={place === "cases"}
          details={verified && !captureCollection && subpage === null && caseFlow === null ? { name: caseTitle, open: () => setFileDetails(true) } : undefined}
          back={
            readerLayout ? null : subpage !== null ? (
              <BackLink label="Captures" onBack={leaveSubpage} />
            ) : verified && caseFlow !== null ? (
              <BackLink label={caseTitle} name={caseTitle} onBack={() => (caseFlow === "requirements" || caseFlow === "value-maps") ? leaveRequirements() : caseFlow === "field-values" ? leaveFieldValues() : caseFlow === "variant" ? variantEditor.leave(() => setCaseFlow(null)) : setCaseFlow(null)} />
            ) : verified && !captureCollection ? (
              <BackLink label="Captures" onBack={backToProject} />
            ) : null
          }
          title={
            readerLayout ? "Messages" : subpage === "capture"
              ? capture.title
              : verified && !captureCollection
                    ? caseFlow === "variant"
                      ? variantEditor.title
                      : caseFlow === "compare"
                        ? caseComparison.title
                        : caseFlow === "value-maps" ? "Value maps" : caseFlow === "requirements" ? "Interface requirements" : caseFlow === "field-values" ? "Field values" : caseTitle
                    : "Captures"
          }
          actions={
            readerLayout ? <>{route.origin?.destination==="environments"?<button type="button" disabled={busy} onClick={()=>void returnFromSetup()}>Return to target</button>:null}<span className="reader-toolbar-edition">HL7 {inspectionResult?.inspection?.metadata.hl7_version || "not declared"}</span><button type="button" className="link reader-toolbar-reference" disabled={busy} onClick={()=>setReaderReferenceRequest(count=>count+1)}>Reference</button><span className="reader-toolbar-space"/><button type="button" disabled={busy} onClick={()=>{setSourceKind("file");open({destination:"inspect-file",view:"messages"});setRawRequest(count=>count+1);}}>Open</button><button type="button" disabled={busy || !root} onClick={startCaptureSetup}>Receive</button><button type="button" disabled={busy || !selectedSourceRef || checkedMessages.size!==2} onClick={()=>setSelectedComparison(true)}>Compare</button><button type="button" className="primary" disabled={busy || !selectedSourceRef} onClick={()=>void createTest()}>Create test case</button><Menu className="reader-menu" trigger={<img className="workbench-icon" src={moreAsset} alt="" />} label="More case actions" items={[...caseMenu,{label:"Receive and send selected",onSelect:()=>setExchangeUIRequest(old=>({kind:"setup",serial:(old?.serial??0)+1})),disabled:busy||!selectedSourceRef||checkedMessages.size===0},{label:"Exchange history",onSelect:()=>setExchangeUIRequest(old=>({kind:"history",serial:(old?.serial??0)+1})),disabled:busy||!selectedSourceRef},{label:"Source context",onSelect:()=>{if(selectedSourceRef)setCaptureContextSource(selectedSourceRef);},disabled:busy || !selectedSourceRef},{label:"Export selected",onSelect:()=>setSelectedExportOpen(true),disabled:busy || !selectedSourceRef || checkedMessages.size===0},{label:"Capture exports",onSelect:()=>setSourceExportsOpen(true),disabled:busy || !selectedSourceRef},{label:"Captures",onSelect:backToProject},{label:"Timeline",onSelect:()=>setView("timeline")},{label:"Findings",onSelect:()=>setView("findings")}]}/></> : subpage === "capture" ? (
              capture.actions
            ) : root && subpage === null && (!verified || captureCollection) ? (
              <>
 <button type="button" disabled={busy} onClick={()=>{setSourceKind("file");open({destination:"inspect-file",view:"messages"});setRawRequest(count=>count+1);}}>Open file</button>
                <button type="button" disabled={busy} onClick={startCaptureSetup}>
                  Capture
                </button>
                <button type="button" className="primary" disabled={busy} onClick={() => setImporting(true)}>
                  Import
                </button>
              </>
            ) : verified && !captureCollection && subpage === null && caseFlow === null ? (
              <>
                {route.origin?.destination==="environments"?<button type="button" disabled={busy} onClick={()=>void returnFromSetup()}>Return to target</button>:null}
                <Menu
                  label="More case actions"
                  items={caseMenu}
                />
              </>
            ) : verified && !captureCollection && subpage === null && caseFlow === "variant" ? (
              variantEditor.actions
            ) : verified && !captureCollection && subpage === null && caseFlow === "compare" ? (
              caseComparison.actions
            ) : null
          }
        >
          {!root ? noProject("cases") : null}
          {capturing && root ? capture.body : null}
          {captureContextSource ? <CaptureSourceContext projectID={currentProject?.ref.id??""} source={captureContextSource} context={exportContext} onClose={()=>setCaptureContextSource(null)} onChanged={()=>void refreshCases()} /> : null}
          {root ? (
            <ImportFlow
              open={importing}
              seed={looseRetention}
              root={root}
              context={importContext}
              drafts={drafts}
              busy={busy}
              onClose={() => {
                setImporting(false);
 if(looseRetention) {setLooseRetention(null);open({destination:"inspect-file",view:"messages"});}
                void leaveImport();
              }}
              onImported={(ref,selection) => {
                setImporting(false);setLooseRetention(null);
                void openImportedCase(ref,selection);
              }}
            />
          ) : null}

          {root && subpage === null && (verified === null || captureCollection) ? (
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
              <CaseList
                toolbarActions={<><IconButton icon="filter" label="Filter cases" onClick={()=>setFilteringCases(true)}/><Menu label="Capture list actions" items={[{label:"Search cases",onSelect:()=>setSearchingCases(true)}]}/></>}
                cases={cases}
                revisions={currentProject?.summary.project?.revisions ?? []}
                notices={caseNotices}
                loading={casesLoading}
                failure={casesFailure}
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

          {verified && root && !captureCollection ? (
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
                        {evidenceFrom === "run" ? "Run evidence" : "Finding evidence"}
                        <IconButton
                          icon="close"
                          label={evidenceFrom === "run" ? "Remove Run evidence" : "Remove Finding evidence"}
                          onClick={() => {
                            setEvidenceFocus(null);
                            if (root) void loadMessages(root, shownCase, messageQuery, messageSort, 0);
                          }}
                        />
                      </span>
                      {evidenceFrom === "run" ? (
                        <button type="button" className="quiet" onClick={() => go("runs")}>
                          Back to run
                        </button>
                      ) : evidenceFrom === "similar" ? (
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
                  {verified.protocol !== "fhir-r4" ? <ExchangePanel
                    context={exchangeContext}
                    readerMode={readerLayout}
                    inspectOwner={verified ? {identity:verified.identity,occurrence:selectedOccurrence??""}:null}
                    request={exchangeUIRequest}
                    onContext={receiveExchangeContext}
                    caseRef={exchangeSource?.project === root && exchangeSource.entry === verified.name && exchangeSource.identity === verified.identity ? exchangeSource.ref : null}
                    selected={sourceSelection.rows.filter(row=>row.kind==="message").map(row=>row.id)}
                    busy={busy}
                    onCreateVariant={() => void startVariant(verified.name, verified.identity, [...checkedMessages], verified.protocol)}
                    requestedExchange={requestedExchange?.project === root ? requestedExchange.origin : null}
                    onCreateTest={(exchange) => {
                      if (!exchange.inputs || !exchange.identity) return;
                      void tests.startNew({ case: exchange.inputs.case, messages: exchange.inputs.messages, exchange: { id: exchange.id, identity: exchange.identity } });
                    }}
                  /> : null}
                  {verified.protocol !== "fhir-r4" && !readerLayout ? (
                  <div className="toolbar toolbar-group source-export-actions">
 <button type="button" disabled={busy || !selectedSourceRef} onClick={()=>setCaptureContextSource(selectedSourceRef)}>Source context</button>
                    <button type="button" disabled={busy || !selectedSourceRef || checkedMessages.size!==2} onClick={()=>setSelectedComparison(true)}>Compare selected</button>
                    <button type="button" disabled={busy || !root} onClick={configureTargetFromSource}>Configure target</button>
                    <button type="button" disabled={busy || !root} onClick={()=>setReceivingSetup(true)}>Receive setup</button>
                    <button type="button" disabled={busy || !selectedSourceRef || checkedMessages.size === 0} onClick={() => setSelectedExportOpen(true)}>Export selected</button>
                    <button type="button" disabled={busy || !selectedSourceRef} onClick={() => setSourceExportsOpen(true)}>Capture exports</button>
                  </div>
                  ) : null}
                  <MessageList
                    browser={readerLayout && !detailOnly}
                    browserSourceName={caseTitle}
                    {...(selectedSourceRef ? {browserSourceKind:selectedSourceRef.kind === "variant" ? "derived" as const : "retained" as const} : {})}
                    {...(verified?.protocol ? { protocol: verified.protocol } : {})}
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
                    sourceSendableCount={sourceSelection.rows.filter(row=>row.kind==="message").length}
 selectionReading={sourceSelection.loading}
 selectionReason={sourceSelection.reason}
                    checked={checkedMessages}
                    onCheck={setCheckedMessages}
                    onCreateTest={() => void createTest()}
                    onSendSelected={() => {
                      const caseRef = listedCases.current.find((item) => item.summary.case?.entry === verified?.name)?.ref;
                      const chosen = sourceSelection.rows.filter(row=>row.kind==="message").map(row=>row.id);
                      if (caseRef && chosen.length > 0) setSendRequest({ kind: "messages", case: { kind: "case", id: caseRef.id }, messages: chosen });
                    }}
                    onCreateVariant={() => verified && void startVariant(verified.name, verified.identity, [...checkedMessages], verified.protocol)}
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
                {caseFlow === "variant" && variantFlow ? <div className="view-panel case-flow">{variantEditor.body}</div> : null}
                {caseFlow === "compare" && compareFlow ? <div className="view-panel case-flow">{caseComparison.body}</div> : null}
                {caseFlow === "requirements" && requirementsContext && root === requirementsContext.workspace && verified.name === requirementsContext.case && verified.identity === requirementsContext.identity ? (
                  <InterfaceRequirements projectID={currentProject?.ref.id??""} context={exportContext} source={requirementsContext.source} identity={requirementsContext.identity}
                    occurrence={requirementsContext.occurrence} selector={requirementsContext.selector} busy={busy} onBack={leaveRequirements} />
                ) : null}
                {caseFlow === "value-maps" && requirementsContext && root === requirementsContext.workspace && verified.name === requirementsContext.case && verified.identity === requirementsContext.identity ? (
                  <ValueMaps context={exportContext} source={requirementsContext.source} identity={requirementsContext.identity} occurrence={requirementsContext.occurrence} selector={requirementsContext.selector} edition={requirementsContext.edition} busy={busy} onBack={leaveRequirements}/>
                ) : null}
                {caseFlow === "field-values" && fieldValuesContext && root === fieldValuesContext.workspace && verified.name === fieldValuesContext.case && verified.identity === fieldValuesContext.identity ? (
                  <FieldValues scope={{ workspace: root, case: verified.name, identity: verified.identity, query: messageQuery }}
                    selector={fieldValuesContext.selector} {...(fieldValuesContext.label ? { label: fieldValuesContext.label } : {})} busy={busy} onBack={leaveFieldValues}
                    onInspectOccurrence={(occurrence, selector) => { setFieldValuesInspecting(true); setRevealed(false); void inspect(occurrence, selector, 0, -1, undefined, false); }} />
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
          title={testsPlace.kind === "list" ? testsView === "suites" ? suites.title : "Test cases" : tests.title}
          details={"details" in tests ? tests.details : undefined}
          back={tests.back}
          actions={
            !root ? null : testsPlace.kind === "list" && testsView === "suites" ? (
              <>
                {suites.actions}
                <button type="button" onClick={()=>open({destination:"runs"})}>Run history</button>
                <button type="button" onClick={()=>openReusable("schedules")}>Schedules</button>
                <Menu label="Reusable work" items={[{label:"Import CI results",onSelect:()=>setCIResultsOpen(true)},{label:"Runners",onSelect:()=>openReusable("settings","runners")},{label:"Team",onSelect:()=>openReusable("settings","team")},{label:"Library",onSelect:()=>open({destination:"library"})}]}/>
                <Menu label="Suite page actions" items={[{ label: "Import suite", onSelect: () => void suites.importSuite() }]} />
              </>
            ) : (
              <>
                {tests.actions}
                {testsPlace.kind === "list" ? <Menu label="Test page actions" items={[
                  ...tests.listTools,
                  {label:"Run history",onSelect:()=>open({destination:"runs"})},
                  {label:"Exports",onSelect:()=>go("reports")},
                  {label:"Schedules",onSelect:()=>openReusable("schedules")},
                  {label:"Import test",onSelect:()=>void tests.importTest(),separated:true},
                  {label:"Import CI results",onSelect:()=>setCIResultsOpen(true)},
                  {label:"Runners",onSelect:()=>openReusable("settings","runners")},
                  {label:"Team",onSelect:()=>openReusable("settings","team")},
                ]}/> : null}
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
                {testsPlace.kind === "list" ? tests.body : null}
              </TaskPanel>
              <TaskPanel tabs="tests-views" tab="suites" className="task-panel view-panel" shown={testsView === "suites"}>
                <div className="toolbar list-toolbar">{suites.toolbar}</div>
                {suitesPlace.kind === "list" ? suites.body : null}
              </TaskPanel>
            </TaskTabs>
          )}
        </Page>

        <Page id="new-test" shown={place === "new-test"} title={tests.title} back={tests.back} actions={<>{tests.actions}{root ? <button type="button" disabled={busy} onClick={()=>setReceivingSetup(true)}>Receive setup</button>:null}</>}>
          {root && place === "new-test" ? tests.body : null}
        </Page>

        <Page id="edit-test" shown={place === "edit-test"} title={tests.title} back={tests.back} actions={<>{tests.actions}{root ? <button type="button" disabled={busy} onClick={()=>setReceivingSetup(true)}>Receive setup</button>:null}</>}>
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
                {libraryView === "profiles" ? <Menu label="More library actions" items={[{ label: "Metadata packs", disabled: busy, onSelect: () => setViewingPacks(true) }, { label: "New FHIR profile", disabled: busy, onSelect: () => void newFHIRProfile() }]} /> : null}
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
              {viewingPacks ? <PacksSheet context={libraryContext} onClose={() => setViewingPacks(false)} onImported={importedLibraryDraft} /> : null}
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
          title={runsPlace.kind === "list" ? runsList.title : runPage.title}
          back={runsPlace.kind === "list" ? undefined : <BackLink label={route.origin ? "Test":"Runs"} onBack={route.origin ? backFromReusable:back} />}
          actions={root ? (runsPlace.kind === "list" ? runsList.actions : runPage.actions) : null}
        >
          {!root ? (
            noProject("runs")
          ) : runsPlace.kind === "list" ? (
            <>
              <div className="toolbar list-toolbar">{runsList.toolbar}</div>
              {runsList.body}
            </>
          ) : (
            runPage.body
          )}
        </Page>

        <Page id="schedules" shown={place === "schedules"} title="Schedules" back={<BackLink label="Back" onBack={backFromReusable} />} actions={schedules.actions}>
          {schedules.body}
        </Page>

        <Page id="compare-runs" shown={place === "compare-runs"} title={runComparison.title} back={<BackLink label={route.origin ? "Test":"Runs"} onBack={route.origin ? backFromReusable:back} />} actions={root ? runComparison.actions : null}>
          {root ? runComparison.body : noProject("runs")}
        </Page>

        <Page id="minimize-failure" shown={place === "minimize-failure"} title={minimize.title} back={<BackLink label="Run" onBack={back} />} actions={root ? minimize.actions : null}>
          {root ? minimize.body : noProject("runs")}
        </Page>

        {root ? (
          <SendReview
            request={sendRequest}
            context={runsContext}
            onClose={() => setSendRequest(null)}
            onBeforeSend={watch}
            onStarted={(started) => {
              setSendRequest(null);
              runActivity.start(started);
              open({ destination: "runs", objectId: "active" });
            }}
            onEditEnvironment={(environment,request) => {
 const from=currentRoute.current;
 setReviewSetup({project:root,request});setSendRequest(null);
 open({destination:"environments",objectId:environment.id,view:"edit",origin:{projectId:root,destination:from.destination,...(from.objectId ? {objectId:from.objectId}:{}),...(from.view ? {view:from.view}:{}),returnContext:leaving(selectedOccurrence??undefined)}});
            }}
            onActivate={() => {
              const requested = sendRequest;
              setSendRequest(null);
              activateFrom(() => setSendRequest(requested));
            }}
          />
        ) : null}

        {root ? (
          <SupportSummarySheet
            open={supportSheet !== null}
            root={root}
            projectId={currentProject?.ref.id ?? ""}
            report={supportSheet?.report ?? null}
            onClose={() => setSupportSheet(null)}
            onRequestApproval={() => {
              setSupportSheet(null);
              void refreshListing();
              open({ destination: "settings", view: "team" });
            }}
          />
        ) : null}

        <Page
          id="environments"
          shown={place === "environments"}
          title={place === "environments" && route.objectId === undefined ? "Targets" : environments.title}
          back={environments.back}
          actions={root ? <>{environments.actions}<button type="button" disabled={busy} onClick={()=>setReceivingSetup(true)}>Receive configurations</button></> : null}
        >
          {opened ? environments.body : noProject("environments")}
        </Page>

        <Page
          id="reports"
          shown={place === "reports"}
          title={reportsPlace.kind === "list" ? "Reports" : reportPage.title}
          back={route.origin ? <BackLink label="Test" onBack={backFromReusable}/> : reportsPlace.kind === "list" ? undefined : <BackLink label="Reports" onBack={back}/>}
          actions={
            root ? (
              reportsPlace.kind === "list" ? (
                <>
                  {reports.actions}
                  <Menu
                    label="More report actions"
                    items={[
                      { label: "Support summary", onSelect: () => setSupportSheet({ report: null }) },
                      { label: "Templates", onSelect: () => open({ destination: "share-templates" }) },
                      { label: "Encrypted packages", onSelect: () => open({ destination: "encrypted-packages" }) },
                    ]}
                  />
                </>
              ) : (
                reportPage.actions
              )
            ) : null
          }
        >
          {!root ? (
            noProject("reports")
          ) : reportsPlace.kind === "list" ? (
            <>
              <div className="toolbar list-toolbar">{reports.toolbar}</div>
              {reports.body}
            </>
          ) : (
            <>
              {reportPage.body}
              {reports.body}
            </>
          )}
        </Page>

        <Page id="share-report" shown={place === "share-report"} title={share.title} back={<BackLink label="Report" onBack={back} />}>
          {root ? share.body : noProject("reports")}
        </Page>

        <Page id="share-templates" shown={place === "share-templates"} title={shareTemplates.title} back={<BackLink label={route.origin ? "Test":"Reports"} onBack={route.origin ? backFromReusable:back} />} actions={root ? shareTemplates.actions : null}>
          {root ? shareTemplates.body : noProject("reports")}
        </Page>

        <Page id="encrypted-packages" shown={place === "encrypted-packages"} title={packages.title} back={<BackLink label={route.origin ? "Test":"Reports"} onBack={route.origin ? backFromReusable:back} />}>
          {root ? packages.body : noProject("reports")}
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
              failure={filesFailure}
              onOpen={(file) => {
                open({ destination: "inspect-file" });
                fileReader.openPath(childPath(root, file.name));
              }}
            />
          ) : (
            noProject("files")
          )}
        </Page>

        <Page id="messages" shown={place === "messages"} title="Messages">
          <EmptyState title="No messages open" action={<><button type="button" onClick={() => { open({ destination: "inspect-file", view: "messages" }); setRawRequest((count) => count + 1); }}>Open file</button>{root ? <button type="button" onClick={() => go("cases")}>Open capture</button> : null}</>} />
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

        <Page id="inspect-file" shown={place === "inspect-file"} title={readerLayout ? "Messages" : fileReader.title} back={readerLayout ? undefined : <BackLink label={route.view === "messages" ? "Messages" : "Tools"} onBack={route.view === "messages" ? () => open({ destination: "messages" }) : back} />} actions={<>{readerLayout ? <><span className="reader-toolbar-edition">HL7 {fileReader.edition || "not declared"}</span><button type="button" className="link reader-toolbar-reference" disabled={busy} onClick={fileReader.chooseReference}>Reference</button><span className="reader-toolbar-space"/></> : null}{fileReader.actions}{readerLayout ? <><button type="button" disabled={busy || !root} onClick={startCaptureSetup}>Receive</button><button type="button" disabled title="Retain the loose file as a capture to compare messages">Compare</button><button type="button" className="primary" disabled title="Retain the loose file as a capture to create a test case">Create test case</button></> : null}{fileReader.retention ? <button type="button" className={readerLayout ? "link" : "primary"} disabled={busy} onClick={()=>{retentionStarted.current="";setLooseRetention(fileReader.retention);if(!root)go("home");}}>Retain capture</button> : null}</>}>
          {fileReader.body}
        </Page>

        <Page id="sample-data" shown={place === "sample-data"} title="Sample data" back={<BackLink label="Tools" onBack={back} />}>
          <EmptyState
            title="Synthetic demo project"
            action={
              <button type="button" className="primary" disabled={busy} onClick={tryDemo}>
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

        <Page id="benchmarks" shown={place === "benchmarks"} title={benchmarks.title} back={<BackLink label="Tools" onBack={back} />} actions={benchmarks.actions}>
          {root ? benchmarks.body : noProject("benchmarks")}
        </Page>

        <Page id="settings" shown={place === "settings"} title="Settings" back={<BackLink label="Back" onBack={backFromReusable}/>} actions={<button type="button" onClick={()=>open({destination:"tools"})}>Tools</button>}>
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
              <section aria-label="Execution setup"><h2>Execution setup</h2><div className="toolbar-group"><button type="button" className="link" onClick={()=>open({destination:"environments"})}>Manage targets</button><button type="button" className="link" onClick={()=>openSettings("team")}>Team and runners</button></div></section>
            </TaskPanel>
            <TaskPanel tabs="settings-views" tab="license" className="task-panel view-panel" shown={settingsView === "license"}>
              <ComputerLicense
                activateRequest={licenseRequest}
                onActivateHandled={() => setLicenseRequest(0)}
                onActivationEnded={(activated) => {
                  const back = licenseReturn.current;
                  licenseReturn.current = null;
                  if (activated) back?.();
                }}
                onAdministratorSetup={() => open({ destination: "license-setup" })}
                onOpenRunners={() => openSettings("runners")}
              />
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
              <RunnerPanel
                root={root}
                request={setupRequests.runner}
                onHandled={setupHandled("runner")}
                onConfigured={configured("runner", "runner:config")}
                onSchedules={(runner) => open({ destination: "schedules", objectId: `runner:${runner}` })}
              />
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

        <Page id="license-setup" shown={place === "license-setup"} title="Administrator setup" back={<BackLink label="Settings" onBack={back} />}>
          <AdministratorSetup portalRequest={setupRequests.portal} onPortalHandled={setupHandled("portal")} onPortalConfigured={configured("portal", "portal")} />
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
                <Menu label="More encryption actions" items={[{ label: "Encrypted packages", onSelect: () => open({ destination: "encrypted-packages" }) }]} />
              </>
            ) : null
          }
        >
          {root ? encryption.body : noProject("encryption")}
        </Page>

        <Page
          id="help"
          shown={place === "help"}
          title="Help"
          actions={
            <>
              <button type="button" onClick={() => setHelpSheet("search")}>
                Search help
              </button>
              <button type="button" disabled={busy} onClick={tryDemo}>
                Try demo
              </button>
              <Menu label="More help actions" items={[{ label: "Diagnostics", onSelect: () => setHelpSheet("diagnostics") }]} />
            </>
          }
        >
          <HelpTopics onOpen={openHelpArticle} onDemo={tryDemo} busy={busy} />
          <SearchHelpSheet open={helpSheet === "search"} onClose={() => setHelpSheet(null)} onOpen={openHelpArticle} />
          <DiagnosticsSheet
            open={helpSheet === "diagnostics"}
            errors={[windowState === "failed" && windowReason ? windowReason : null, openNotice?.reason ?? null, demoNotice].filter((error): error is string => !!error)}
            privacy={described?.privacy}
            onClose={() => setHelpSheet(null)}
            onExport={
              root
                ? () => {
                    setHelpSheet(null);
                    setSupportSheet({ report: null });
                  }
                : undefined
            }
          />
        </Page>

        <Page id="help-article" shown={place === "help-article"} title={helpArticle.title} back={<BackLink label="Help" onBack={back} />} actions={helpArticle.actions}>
          {helpArticle.body(openHelpArticle)}
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
        {fileReader.details ? cloneElement(fileReader.details,{toolbarReference:!detailOnly,compactReader,onCompactReader:()=>setReaderDensityOverride(!compactReader)}) : null}
      </>
    ) : benchmarkSelection ? (
      <>
        {detailOnly ? <BackLink label="Benchmarks" onBack={benchmarks.closeDetails} /> : null}
        {benchmarks.details}
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
            key={`${root ?? ""}/${messages?.case ?? verified?.name ?? ""}/${messages?.identity ?? verified?.identity ?? ""}`}
            onValidationFindings={()=>setView("findings")}
            {...(root && verified.protocol !== "fhir-r4" ? {onCreateVariant:()=>void startVariant(verified.name,verified.identity,checkedMessages.size ? [...checkedMessages] : selectedOccurrence ? [selectedOccurrence] : [],verified.protocol)} : {})}
            compactReader={compactReader}
            onCompactReader={()=>setReaderDensityOverride(!compactReader)}
            toolbarReference={readerLayout && !detailOnly}
            referenceRequest={readerReferenceRequest}
            contextDetails={exchangeDetails?.key===JSON.stringify([selectedSourceRef?.id??"",verified.identity,selectedOccurrence??""])?exchangeDetails.node:undefined}
            contextDetailsTitle="Recorded exchange"
            result={inspectionResult}
            referenceCatalog={referenceCatalog.current}
            referenceIdentity={referenceIdentity.current}
            {...(referenceSelection.current ? { referenceSelection: referenceSelection.current } : {})}
            {...(selectedRow ? { kind: selectedRow.kind, source: selectedRow.source_name || selectedRow.source_id } : {})}
            loading={running === "inspect"}
            busy={busy}
            onInspect={(path, nodeOffset, byteOffset, rawOffset, catalogPath?: string, selection?: HL7ReferenceSelection, catalogIdentity?: string) => (selectedOccurrence ? inspect(selectedOccurrence, path, nodeOffset, byteOffset, undefined, revealed, rawOffset, catalogPath, selection, catalogIdentity) : Promise.resolve(null))}
            onReveal={(next) => {
              setRevealed(next);
              const at = inspectionResult?.inspection;
              if (selectedOccurrence) void inspect(selectedOccurrence, at?.fhir?.selected?.field.id ?? at?.selected.path ?? "", at?.node_offset ?? 0, at?.byte_offset ?? -1, undefined, next);
            }}
            {...(root && verified.protocol !== "fhir-r4" && caseFlow === null ? { onFieldValues: openFieldValues, onValueMaps:(selector:string)=>openRequirements(selectedOccurrence??"",selector,"value-maps"), onRequirements: (selector: string) => openRequirements(selectedOccurrence ?? "", selector) } : {})}
            onFilterByField={(selector: string, value: string | null, state: FieldState) => {
              setView("messages");
              setFilterSeed({ selector, value, state });
            }}
            onInspectResource={(occurrence) => inspect(occurrence, "", 0, -1)}
            onCreateFHIRCheck={(preset) => {
              const expected: DatasetValue = { state: "present", type: preset.value_type, ...(preset.code_system ? { code_system: preset.code_system } : {}) };
              const check = {
                ...preset.assertion,
                ...(preset.repeated
                  ? (preset.assertion.sequence === undefined ? { sequence: [expected] } : {})
                  : (preset.assertion.expected === undefined ? { expected } : {})),
              };
              setLibraryImport({
                state: "completed", context: libraryContext(), new: true,
                draft: { name: "", check_group: {
                  set: { schema: "readmit-assertion-set-draft/v1", name: "", assertions: [] }, unsupported: [],
                  fhir: { schema: "readmit-fhir-check-group/v1", name: "", set: {
                    schema: "readmit-dataset-assertion-set/v1", bindings: [preset.binding], assertions: [check],
                  }, projections: [preset.projection] },
                } },
              });
              open({ destination: "library", view: "checks", objectId: "import" });
            }}
            onClose={closeDetails}
            {...(detailOnly ? { backLabel: caseFlow === "field-values" ? "Field values" : caseView === "timeline" ? "Timeline" : "Messages", onBack: closeDetails } : {})}
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
        return root !== null && verified !== null && verified.protocol !== "fhir-r4";
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

  const panes = { "--inspector-width": `${inspectorRem}rem`, "--message-browser-width": `${messageBrowserRem}rem` } as CSSProperties;

  return (
    <IndicatorsContext.Provider value={indicators}>
      <VocabularyContext.Provider value={described.vocabulary}>
        <FrameContext.Provider value={{ compact, switcher }}>
          <ReturnAnchor.Provider value={returnAnchor}>
          <PaletteActionsContext.Provider value={registerActions}>
              <div className={`${compact ? "app compact" : "app"}${root ? " app-workbench" : ""}${readerLayout ? ` app-reader${compactReader ? " app-reader-compact" : " app-reader-wide"}` : ""}`} style={readerLayout ? { "--sidebar": `${sidebarRem}rem`, "--reader-reference-width": `${compactReader ? READER_REFERENCE_COMPACT_REM : READER_REFERENCE_REM}rem` } as CSSProperties : undefined} ref={setFrame} onScrollCapture={(event) => { if (event.target instanceof HTMLElement && event.target.classList.contains("page-body")) setScrollRevision((revision) => revision + 1); }}>
                <nav className="sidebar" aria-label="Main">
                  {region("navigation")}
                </nav>
                <div className={`workarea${detailsShown ? (detailOnly ? " detail-only" : " with-details") : ""}${readerLayout ? " reader-layout" : ""}`} style={panes}>
                  {region("evidence", detailOnly)}
                  {detailsShown && !detailOnly ? (
                    <Separator
                      value={readerLayout ? readerRem : inspectorRem}
                      min={readerLayout ? readerMinRem : INSPECTOR_MIN_REM}
                      max={readerLayout ? readerMaxRem : INSPECTOR_MAX_REM}
                      step={INSPECTOR_STEP}
                      onChange={(width) => {
                        if (readerLayout) setMessageBrowserWidths((held) => ({ ...held, [root ?? "standalone-file"]: workareaRem - width - 1 / rem }));
                        else if (root !== null) setInspectorWidths((held) => ({ ...held, [root]: width }));
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

        {selectedComparison && selectedSourceRef ? <SelectedMessageComparison source={selectedSourceRef} identity={verified?.identity??""} messages={[...checkedMessages]} context={exportContext} onClose={()=>setSelectedComparison(false)} onCreateTest={()=>{setSelectedComparison(false);void createTest();}} />:null}
        {receivingSetup && root ? <ReceiveConfigurations context={captureContext} onClose={()=>setReceivingSetup(false)} />:null}
        {selectedExportOpen ? <SelectedMessagesExportPanel open context={exportContext} source={selectedSourceRef} identity={verified?.identity ?? ""} messages={[...checkedMessages]} sourceName={caseTitle} onClose={() => setSelectedExportOpen(false)} /> : null}
        {ciResultsOpen ? <CIResultsSheet onClose={()=>setCIResultsOpen(false)}/>:null}
        {sourceExportsOpen && selectedSourceRef ? <SourceExportHistory open context={exportContext} source={selectedSourceRef} onClose={() => setSourceExportsOpen(false)} /> : null}
        <Modal open={Boolean(unavailableMapDraft)} title="Retained value map draft" onClose={()=>setUnavailableMapDraft(null)} footer={<button type="button" onClick={async()=>{if(!unavailableMapDraft)return;const id=unavailableMapDraft.draft.id;const result=await discardEditorDraft(id);if(result.state!=="completed"){setUnavailableMapDraft(held=>held?{...held,reason:result.reason||"The private draft could not be discarded."}:null);return;}setDrafts(held=>held?.filter(draft=>draft.id!==id)||null);setUnavailableMapDraft(null);}}>Discard retained map draft</button>}>
          <p>{unavailableMapDraft?.reason}</p><p>This is authored declaration content only. No contextual value or consent is restored.</p>
          {unavailableMapDraft?<pre className="value">{JSON.stringify(unavailableMapDraft.draft.content,null,2)}</pre>:null}
        </Modal>
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
                <dt>{verified.protocol === "fhir-r4" ? "Resources" : "Messages"}</dt>
                <dd>{verified.protocol === "fhir-r4" ? verified.resources : verified.messages}</dd>
              </div>
              {verified.protocol === "fhir-r4" ? <div className="fact">
                <dt>Requests</dt>
                <dd>{verified.requests}</dd>
              </div> : <div className="fact">
                <dt>ACKs</dt>
                <dd>{verified.acknowledgements}</dd>
              </div>}
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
          {found && (found.state === "completed" || found.state === "empty") && found.matches.length === 0 ? <p className="hint">No matches.</p> : null}
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
                  activateFrom(() => setCreatingProject(true));
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
  [SHARE_DRAFT_KIND]: { route: { destination: "share-report" } },
  [VARIANT_DRAFT_KIND]: { case: "variant" },
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
  if(draft.kind==="interface-association")return draft.content_schema==="readmit-interface-association-editor/v1";
  if(draft.kind==="capture-source-context")return draft.content_schema==="readmit-capture-source-context-editor/v1";
  if(draft.kind==="field-value-map")return draft.content_schema==="readmit-field-value-map-editor/v1";
  if (SHEET_DRAFTS.has(draft.kind)) return draft.item !== undefined;
  // A test draft reopens only in the editor that wrote it; an earlier
  // release's test drafts are offered for Discard.
  if (draft.kind === "test-draft") return draft.content_schema === TEST_EDITOR_DRAFT || draft.content_schema === CONNECTED_EDITOR_DRAFT || draft.content_schema === EXCHANGE_EDITOR_DRAFT;
  if (draft.kind === "suite-editor") return draft.content_schema === SUITE_EDITOR_DRAFT || draft.content_schema === CONNECTED_SUITE_EDITOR_DRAFT;
  if (draft.kind === SCENARIO_DRAFT_KIND) return draft.content_schema === SCENARIO_DRAFT_SCHEMA && scenarioDraftObject(draft) !== undefined;
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
