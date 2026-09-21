import { ContextHelp } from "./ContextHelp";
import { OperationAccess } from "./OperationAccess";
import { HubPanel } from "./HubPanel";
import { RunComparison } from "./RunComparison";
import { Baseline } from "./Baseline";
import { NoteDraft } from "./NoteDraft";
import { Recovery, RetainedDrafts } from "./Recovery";
import { RunPanel } from "./RunPanel";
import { EnvironmentPanel } from "./EnvironmentPanel";
import { onRetentionResult, savedId } from "./drafting";
import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  type CorrelationReviewResult,
  type SequenceResult,
  openReview,
  previewTransformation,
  type ReviewResult,
  type TransformResult,
  inspectOccurrence,
  type InspectionResult,
  openProjectOverview,
  createProject,
  updateProjectSettings,
  registerCase,
  updateRegisteredCase,
  openWorkspace,
  buildIndex,
  describeIndex,
  type BuildIndexRequest,
  type IndexResult,
  recentWorkspaces,
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
  type RegionId,
  type SearchResult,
  type Shell,
  type State,
  type StatusValue,
  type Theme,
  type WorkspaceResult,
} from "./bindings";
import { Comparison, COMPARISON_WINDOW } from "./Comparison";
import { Review, REVIEW_WINDOW } from "./Review";
import { GuidedSample } from "./GuidedSample";
import { Sequence, SEQUENCE_WINDOW } from "./Sequence";
import { Inspector } from "./Inspector";
import { Reproducer } from "./Reproducer";
import { RevisionComparison } from "./RevisionComparison";
import { CanonicalTestEditor } from "./CanonicalTestEditor";
import { ProfileEditor } from "./ProfileEditor";
import { TestAuthoring } from "./TestAuthoring";
import { Badge, GRID_WINDOW, MessageGrid, Palette, Report, Separator, Status } from "./shell";
import { Breadcrumbs, ProjectPanel } from "./ProjectPanel";
import { ImportPanel } from "./ImportPanel";
import { CapturePanel } from "./CapturePanel";

/** The panes never collapse to nothing: either one keeps a usable share of the
 * window, whether it is dragged or moved with a keyboard. */
const MIN_SPLIT = 20;
const MAX_SPLIT = 80;
const SPLIT_STEP = 5;

/** Verifying a case, reading a project and searching all run to completion once
 * they start, so Cancel is offered only while an interruptible operation runs. */
type Running =
  | null
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
  | "transformation"
  | "review";

export default function App() {
  const [described, setDescribed] = useState<Shell | null>(null);
  const [windowState, setWindowState] = useState<State>("busy");
  const [windowReason, setWindowReason] = useState<string | undefined>(undefined);

  const [running, setRunning] = useState<Running>(null);
  const [workspace, setWorkspace] = useState<WorkspaceResult | null>(null);
  const [evidence, setEvidence] = useState<CaseResult | null>(null);
  const [investigation, setInvestigation] = useState<ProjectOverviewResult | null>(null);
  const [recent, setRecent] = useState<RecentResult | null>(null);
  const [found, setFound] = useState<SearchResult | null>(null);
  const [savedFilters, setSavedFilters] = useState<FiltersResult | null>(null);
  const [inspectionResult, setInspectionResult] = useState<InspectionResult | null>(null);
  const [selectedOccurrence, setSelectedOccurrence] = useState<string | null>(null);
  const [gridResult, setGridResult] = useState<GridResult | null>(null);
  const [indexResult, setIndexResult] = useState<IndexResult | null>(null);
  const [reproducerResult, setReproducerResult] = useState<ReproducerResult | null>(null);
  const [testResult, setTestResult] = useState<TestResult | null>(null);
  const [comparisonResult, setComparisonResult] = useState<CompareResult | null>(null);
  const [revisionResult, setRevisionResult] = useState<ReproducerComparisonResult | null>(null);
  const [guideResult, setGuideResult] = useState<GuideResult | null>(null);
  const [practiceResult, setPracticeResult] = useState<PracticeResult | null>(null);
  const [sequenceResult, setSequenceResult] = useState<SequenceResult | null>(null);
  const [transformResult, setTransformResult] = useState<TransformResult | null>(null);
  const [reviewResult, setReviewResult] = useState<ReviewResult | null>(null);
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

  const regionElements = useRef<Partial<Record<RegionId, HTMLElement | null>>>({});
  const searchField = useRef<HTMLInputElement | null>(null);

  const indicators = useMemo(() => {
    const table = new Map<StatusValue, Indicator>();
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
  const watch = useCallback(async (folder: string) => {
    setWatchedRun(folder);
    await pushRecord(view(folder));
  }, [pushRecord, view]);

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

  const busy = running !== null;
  const root = workspace?.workspace?.root ?? null;
  const opened = workspace?.workspace;
  const overview = investigation?.overview ?? null;
  const verified = evidence?.case ?? null;

  const focusRegion = useCallback((region: RegionId) => {
    regionElements.current[region]?.focus();
  }, []);

  const step = useCallback(
    (direction: number) => {
      const regions = described?.regions ?? [];
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

  // One operation runs at a time here as well as in the facade. The slot is
  // released whatever happens, including a rejection the boundary did not turn
  // into a failed result, so nothing can leave the window permanently disabled.
  const operate = useCallback(async (kind: Exclude<Running, null>, work: () => Promise<void>) => {
    setRunning(kind);
    try {
      await work();
    } finally {
      setRunning(null);
    }
  }, []);

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
    setTransformResult(null);
    setReviewResult(null);
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
      await operate("workspace", async () => {
        const result = await operation();
        if (result.workspace) {
          clearWorkspace();
          setSelected(null);
          setWorkspace(result);
          setPracticeResult(null);
          await refreshGuide(result.workspace.root);
        } else {
          setOpenNotice(result);
        }
      });
      await refreshRecent();
      focusRegion("navigation");
    },
    [clearWorkspace, focusRegion, operate, refreshGuide, refreshRecent],
  );

  // Reads whose answer can be older than the question: only the response to
  // the latest request commits, so a late answer to an earlier one is dropped
  // instead of overwriting what the viewer asked for last.
  const readTickets = useRef(new Map<string, number>());
  const currentTicket = useCallback((kind: string) => {
    const ticket = (readTickets.current.get(kind) ?? 0) + 1;
    readTickets.current.set(kind, ticket);
    return ticket;
  }, []);
  const isCurrent = useCallback(
    (kind: string, ticket: number) => readTickets.current.get(kind) === ticket,
    [],
  );

  // One window of the grid at a time. Asking for the next one re-reads the case
  // and its index, so a window is never served from an index the evidence no
  // longer supports.
  const showGrid = useCallback(
    async (folder: string, name: string, indexName: string, offset: number) => {
      const ticket = currentTicket("grid");
      await operate("grid", async () => {
        setGridResult(null);
        setInspectionResult(null);
        setSelectedOccurrence(null);
        const result = await openGrid(folder, name, indexName, offset, GRID_WINDOW);
        if (isCurrent("grid", ticket)) {
          setGridResult(result);
        }
      });
    },
    [currentTicket, isCurrent, operate],
  );

  const handleBuildIndex = useCallback(
    async (request: BuildIndexRequest) => {
      if (!root) return;
      let builtIndexName: string | null = null;
      await operate("grid", async () => {
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
    [operate, root, showGrid],
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
      await operate("case", async () => {
        const result = await openCase(folder, name);
        if (result.case) {
          clearCase();
          setSelected(name);
          setEvidence(result);
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
      focusRegion("inspector");
      return outcome;
    },
    [clearCase, focusRegion, operate, showGrid],
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
      const ticket = currentTicket("inspect");
      await operate("inspect", async () => {
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
        if (isCurrent("inspect", ticket)) {
          setInspectionResult(result);
        }
      });
    },
    [currentTicket, evidence, gridResult, isCurrent, operate, root],
  );

  // One practice run of the guided sample. It is the one operation in this
  // window that sends, so Cancel is offered while it runs, and what the folder
  // then holds is read back rather than assumed from the result.
  const practise = useCallback(
    async (trial: GuideTrialId, output: string) => {
      if (!root || !guideResult?.guide?.spec) return;
      const spec = guideResult.guide.spec;
      await operate("practice", async () => {
        setPracticeResult(null);
        setPracticeResult(await runPractice({ workspace: root, spec, trial, output }));
      });
      await refreshGuide(root);
    },
    [guideResult, operate, refreshGuide, root],
  );

  // The project overview re-reads the project from disk every time: what the
  // window shows is what the project records, never what an earlier action
  // reported. Every write below returns the refreshed overview, so a panel is
  // never left beside state the project has moved past.
  const readProject = useCallback(
    async (folder: string) => {
      await operate("project", async () => {
        setInvestigation(null);
        setInvestigation(await openProjectOverview(folder));
      });
      focusRegion("evidence");
    },
    [focusRegion, operate],
  );

  const startProject = useCallback(
    async (name: string, title: string, owner: string, versions: string[]) => {
      await operate("project", async () => {
        const result = await createProject(name, title, owner, versions);
        setInvestigation(result);
      });
    },
    [operate],
  );

  const editSettings = useCallback(
    async (change: SettingsChange) => {
      if (!root) return;
      await operate("project", async () => {
        setInvestigation(await updateProjectSettings(root, change));
      });
    },
    [operate, root],
  );

  const register = useCallback(
    async (name: string, registration: CaseRegistration) => {
      if (!root) return;
      await operate("project", async () => {
        setInvestigation(await registerCase(root, name, registration));
      });
    },
    [operate, root],
  );

  const updateCase = useCallback(
    async (name: string, change: CaseChange) => {
      if (!root) return;
      await operate("project", async () => {
        setInvestigation(await updateRegisteredCase(root, name, change));
      });
    },
    [operate, root],
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
          }
          focusRegion("inspector");
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
      focusRegion(match.region);
    },
    [busy, focusRegion, inspect, opened, readProject, root, verifyCase],
  );

  // Breadcrumb back navigation: out of the case to the project, and out of
  // the project to the folder listing. The trail is the way out as well as
  // the way in.
  const backToProject = useCallback(() => {
    setImporting(false);
    clearCase();
    focusRegion("evidence");
  }, [clearCase, focusRegion]);

  const backToWorkspace = useCallback(() => {
    setImporting(false);
    clearWorkspace();
    setSelected(null);
    setInvestigation(null);
    focusRegion("navigation");
  }, [clearWorkspace, focusRegion]);

  const searchWorkspace = useCallback(
    async (folder: string, wanted: string) => {
      await operate("search", async () => {
        setFound(null);
        setFound(await search(folder, wanted));
      });
    },
    [operate],
  );

  // A comparison is bound to the identity the window verified for the open
  // case, so one is never shown beside counts from evidence that has changed.
  // Asking for the next window is another comparison: both collections are
  // read and aligned again rather than a row list being held here.
  const compareCollections = useCallback(
    async (right: string, keys: string[], fields: string[], offset: number) => {
      const open = evidence?.case;
      if (!root || !open) return;
      await operate("comparison", async () => {
        setComparisonResult(null);
        setComparisonResult(
          await compare({
            workspace: root,
            left: open.name,
            identity: open.identity,
            right,
            keys,
            fields,
            offset,
            limit: COMPARISON_WINDOW,
          }),
        );
      });
    },
    [evidence, operate, root],
  );

  // A sequence is bound to the identity the window verified for the open case,
  // so one is never drawn beside counts from evidence that has changed. Asking
  // for the next window is another sequence: the case is verified and the rules
  // are read again rather than an event list being held here.
  const layOutSequence = useCallback(
    async (rules: string, offset: number, analysis = "") => {
      const open = evidence?.case;
      if (!root || !open) return;
      await operate("sequence", async () => {
        setSequenceResult(null);
        setSequenceResult(
          await openSequence({
            workspace: root,
            case: open.name,
            identity: open.identity,
            rules,
            analysis,
            offset,
            limit: SEQUENCE_WINDOW,
          }),
        );
      });
    },
    [evidence, operate, root],
  );

  // Comparing two built revisions reads two finished reproducers and the runs
  // retained for them. It is bound to nothing the window is holding: both are
  // named entries of the open workspace and are verified again on every call.
  const compareRevisions = useCallback(
    async (left: string, right: string, leftResult: string, rightResult: string) => {
      if (!root) return;
      await operate("revisions", async () => {
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
    [operate, root],
  );

  // A transformation preview is bound to the identity the window verified for
  // the open case, so a plan is never previewed against evidence that changed
  // since. It writes nothing at all: the documents are read again on every call
  // and no preview is held here.
  const previewPlan = useCallback(
    async (rules: string, plan: string, profile: string) => {
      const open = evidence?.case;
      if (!root || !open) return;
      await operate("transformation", async () => {
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
    [evidence, operate, root],
  );

  // An export review is read again on every call, including for the next window
  // of its inventory, so the identity an approval names is always the one the
  // bytes on disk have now. The approval a person typed is sent and checked; it
  // is never retained here or anywhere else.
  const readReview = useCallback(
    async (review: string, approve: string, offset: number) => {
      if (!root) return;
      await operate("review", async () => {
        setReviewResult(null);
        setReviewResult(
          await openReview({
            workspace: root,
            review,
            approve,
            offset,
            limit: REVIEW_WINDOW,
          }),
        );
      });
    },
    [operate, root],
  );

  // A reproducer plan is bound to the case the grid verified, so every step
  // carries the plan back to the engine, which decides what it means. The
  // window keeps no second copy of the selection or of what a dependency added.
  // A plan the engine accepted is retained as an unstored editor draft under an
  // internal identity, so the editing survives an interruption; a build writes
  // the real manifest beside the evidence and drops the draft.
  const reproducerDraftId = useRef("");
  const testDraftId = useRef("");

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
      await operate("reproducer", async () => {
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
    [dropReproducerDraft, gridResult, keepReproducerDraft, operate, reproducerResult, root],
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
      await operate("authoring", async () => {
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
        // A saved spec is a step of the guided sample, so what that folder now
        // holds is read again rather than inferred from this call succeeding.
        if (result.test?.output) {
          await refreshGuide(root);
        }
      });
    },
    [gridResult, operate, refreshGuide, root, testResult],
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
      await operate("filters", async () => {
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
    [gridResult, operate, root, showGrid],
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
      focusRegion("commands");
      searchField.current?.focus();
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
      focusRegion("inspector");
    },
    "cancel-operation": cancel,
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
        // The palette is a modal dialog and closes itself on Escape, so the
        // key only cancels an operation while the palette is not open.
        if (event.key === "Escape") {
          return paletteOpen ? null : "cancel-operation";
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

  // Every region the facade declares has an element here, for the same reason
  // every command has an action: a region the window forgot is a type error.
  // ReactElement rather than ReactNode, because ReactNode admits null and would
  // accept a region entered as nothing. What a region then draws is beyond it.
  const content: Record<RegionId, ReactElement> = {
    commands: (
      <>
        <div className="actions">
          <button type="button" disabled={busy} onClick={actions["open-workspace"]}>
            Open a workspace folder…
          </button>
          <button type="button" disabled={busy} onClick={actions["create-sample-workspace"]}>
            Create the sample workspace…
          </button>
          <button type="button" onClick={actions["command-palette"]}>
            Commands (Ctrl+K)
          </button>
          <button type="button" disabled={running !== "workspace"} onClick={cancel}>
            Cancel
          </button>
        </div>
        <div className="appearance">
          <label htmlFor="theme">Theme</label>
          <select id="theme" value={theme} onChange={(event) => setTheme(event.target.value as Theme)}>
            {(described?.themes ?? []).map((choice) => (
              <option key={choice} value={choice}>
                {choice}
              </option>
            ))}
          </select>
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
        <form
          className="search"
          role="search"
          onSubmit={(event) => {
            event.preventDefault();
            if (root && !busy) {
              void searchWorkspace(root, query);
            }
          }}
        >
          <label htmlFor="workspace-search">Search this workspace</label>
          <input
            id="workspace-search"
            ref={searchField}
            type="search"
            value={query}
            disabled={!root || busy}
            onChange={(event) => setQuery(event.target.value)}
          />
          <button type="submit" disabled={!root || busy}>
            Search
          </button>
        </form>
        {root ? null : <p className="hint">Open a workspace folder to search what it holds.</p>}
        <Report
          indicators={indicators}
          progress={running === "search" ? "Searching this workspace." : null}
          result={found && found.state !== "completed" ? found : null}
        />
        {found && found.matches.length > 0 ? (
          <ul className="matches" aria-label="Search results">
            {found.matches.map((match) => (
              <li key={`${match.kind}:${match.name}:${match.field}:${match.occurrence ?? ""}`}>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    setSelected(match.name);
                    openMatch(match);
                  }}
                >
                  <span className="name">{match.label}</span>
                  <span className={`match-badge ${match.kind === "content" ? "content-badge" : "metadata-badge"}`}>
                    {match.kind === "content" ? "[content]" : "[metadata]"}
                  </span>
                  <span className="reason">matched its {match.field}</span>
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </>
    ),
    navigation: (
      <>
        <GuidedSample
          result={guideResult}
          practice={practiceResult}
          busy={busy}
          progress={running === "practice" ? "Running the saved test against the practice receiver." : null}
          indicators={indicators}
          onCreateSample={actions["create-sample-workspace"]}
          onOpenCase={(name: string) => {
            if (root) void verifyCase(root, name);
          }}
          onRun={(trial, output) => void practise(trial, output)}
        />
        <Report
          indicators={indicators}
          progress={running === "workspace" ? "Opening the folder." : null}
          result={workspace}
        />
        {openNotice ? (
          <Report indicators={indicators} progress={null} result={openNotice} />
        ) : null}
        {opened ? <p className="root">{opened.root}</p> : null}
        {opened && opened.artifacts.length > 0 ? (
          <ul className="artifacts">
            {opened.artifacts.map((artifact) => (
              <li key={artifact.name} aria-current={selected === artifact.name ? "true" : undefined}>
                <span className="name">{artifact.name}</span>
                <Badge indicator={indicators.get(artifact.kind)} fallback={artifact.kind} />
                {artifact.kind === "case" ? (
                  <>
                    <span className="badge">{artifact.schema}</span>
                    <span className="badge">{artifact.provenance}</span>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => void verifyCase(opened.root, artifact.name)}
                    >
                      Verify and open
                    </button>
                  </>
                ) : null}
                {artifact.kind === "project" ? (
                  <button type="button" disabled={busy} onClick={() => void readProject(opened.root)}>
                    Read the project
                  </button>
                ) : null}
                {artifact.kind === "target" ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      setSelected(artifact.name);
                      setEnvironmentArtifact({ name: artifact.name, kind: "target" });
                    }}
                  >
                    Configure target
                  </button>
                ) : null}
                {artifact.kind === "secret" ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      setSelected(artifact.name);
                      setEnvironmentArtifact({ name: artifact.name, kind: "secrets" });
                    }}
                  >
                    Manage secrets
                  </button>
                ) : null}
                {artifact.kind === "policy" ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      setSelected(artifact.name);
                      setEnvironmentArtifact({ name: artifact.name, kind: "policy" });
                    }}
                  >
                    Review policy
                  </button>
                ) : null}
                {artifact.kind === "reset" ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      setSelected(artifact.name);
                      setEnvironmentArtifact({ name: artifact.name, kind: "reset" });
                    }}
                  >
                    Review reset plan
                  </button>
                ) : null}
                {artifact.kind === "unsupported" ? (
                  <span className="unsupported">{artifact.reason}</span>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
        <h3>Recent workspaces</h3>
        {recent && recent.state !== "completed" ? (
          <Status indicator={indicators.get(recent.state)} state={recent.state} reason={recent.reason} />
        ) : null}
        <ul className="recent">
          {(recent?.roots ?? []).map((folder) => (
            <li key={folder}>
              <button
                type="button"
                disabled={busy}
                onClick={() => void openFolder(() => openWorkspace(folder))}
              >
                {folder}
              </button>
            </li>
          ))}
        </ul>
      </>
    ),
    evidence: (
      <>
        <Breadcrumbs
          project={overview?.title ?? null}
          selectedCase={verified?.name ?? null}
          importing={importing}
          capturing={capturing}
          onWorkspace={backToWorkspace}
          onProject={backToProject}
        />
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
              void verifyCase(root, name);
            }}
            onSetupIndex={(name) => {
              setImporting(false);
              void verifyCase(root, name);
            }}
            onClose={() => setImporting(false)}
          />
        ) : (
          <>
            <Recovery
              restored={restored}
              onChanged={() => void restore()}
              onReopen={() => void reopen()}
            />
            <RetainedDrafts drafts={drafts} onDiscardDraft={dropDraft} />
            <NoteDraft project={workspaceRoot} drafts={drafts} restored={restored} onChanged={() => void restore()} />
            <RunPanel onWatch={watch} />
            <ProjectPanel
              root={root}
              result={investigation}
              entries={opened?.artifacts ?? []}
              busy={busy}
              progress={running === "project" ? "Reading the project." : null}
              indicators={indicators}
              selectedCase={verified?.name ?? null}
              onCreate={(name, title, owner, versions) => void startProject(name, title, owner, versions)}
              onUpdateSettings={(change) => void editSettings(change)}
              onRegister={(name, registration) => void register(name, registration)}
              onUpdateCase={(name, change) => void updateCase(name, change)}
              onOpenCase={(name: string) => {
                if (root) void verifyCase(root, name);
              }}
              onStartImport={() => setImporting(true)}
              onStartCapture={() => setCapturing(true)}
            />
          </>
        )}
      </>
    ),
    inspector: (
      <>
        {root ? <Baseline key={root} workspace={root} busy={busy} /> : null}
        {root ? <RunComparison key={"runs-" + root} workspace={root} busy={busy} /> : null}
        <Report
          indicators={indicators}
          progress={running === "case" ? "Verifying the case." : null}
          result={evidence}
        />
        {caseNotice ? (
          <Report indicators={indicators} progress={null} result={caseNotice} />
        ) : null}
        {evidence?.case ? (
          <dl className="evidence">
            <dt>Name</dt>
            <dd>{evidence.case.name}</dd>
            <dt>Contract</dt>
            <dd>{evidence.case.schema}</dd>
            <dt>Provenance</dt>
            <dd>{evidence.case.provenance}</dd>
            <dt>Identity</dt>
            <dd className="identity">{evidence.case.identity}</dd>
            <dt>Sources</dt>
            <dd>{evidence.case.sources}</dd>
            <dt>Occurrences</dt>
            <dd>{evidence.case.occurrences}</dd>
            <dt>Messages</dt>
            <dd>{evidence.case.messages}</dd>
            <dt>Acknowledgements</dt>
            <dd>{evidence.case.acknowledgements}</dd>
            <dt>Unparsed</dt>
            <dd>{evidence.case.unparsed}</dd>
          </dl>
        ) : null}
        {verified ? (
          <MessageGrid
            indicators={indicators}
            progress={running === "grid" ? "Reading this window of the case." : null}
            result={gridResult}
            filters={savedFilters}
            entries={(opened?.artifacts ?? [])
              .filter((artifact) => artifact.kind === "index")
              .map((artifact) => artifact.name)}
            busy={busy}
            onOpen={(indexName, offset) => {
              if (root) {
                void (async () => {
                  const desc = await describeIndex(root, verified.name, indexName);
                  setIndexResult(desc);
                  void showGrid(root, verified.name, indexName, offset);
                })();
              }
            }}
            onSelect={(name) => void changeFilters(() => selectFilter(name))}
            onSave={(filter) => void changeFilters(() => saveFilter(filter))}
            selectedOccurrence={selectedOccurrence}
            onInspect={(occurrence) => void inspect(occurrence, "", 0, -1)}
            caseEvidence={evidence?.case}
            indexDetails={indexResult?.index}
            onBuildIndex={handleBuildIndex}
          />
        ) : null}
        {verified ? (
          <Inspector
            result={inspectionResult}
            busy={busy}
            progress={
              running === "inspect" ? "Verifying and inspecting the selected occurrence." : null
            }
            indicators={indicators}
            onInspect={(path, nodeOffset, byteOffset) => {
              if (selectedOccurrence)
                void inspect(selectedOccurrence, path, nodeOffset, byteOffset);
            }}
          />
        ) : null}
        {gridResult?.grid ? (
          <Reproducer
            rows={gridResult.grid.rows}
            result={reproducerResult}
            restoredDraft={Boolean(reproducerResult?.reproducer && !reproducerResult.reproducer.resolution)}
            onDiscardDraft={discardReproducerDraft}
            inspected={
              selectedOccurrence && inspectionResult?.inspection
                ? {
                    occurrence: selectedOccurrence,
                    path: inspectionResult.inspection.selected.path,
                  }
                : null
            }
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
          />
        ) : null}
        {root ? <CanonicalTestEditor key={"editor-" + root} workspace={root} drafts={drafts} busy={busy} /> : null}
        {root ? <ProfileEditor key={`profile-${root}`} workspace={root} drafts={drafts} busy={busy} /> : null}
        {gridResult?.grid ? (
          <TestAuthoring
            rows={gridResult.grid.rows}
            result={testResult}
            restoredDraft={Boolean(testResult?.test && !testResult.test.resolution)}
            onDiscardDraft={discardTestDraft}
            inspected={
              selectedOccurrence && inspectionResult?.inspection
                ? {
                    occurrence: selectedOccurrence,
                    path: inspectionResult.inspection.selected.path,
                  }
                : null
            }
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
        ) : null}
        {verified ? (
          <Sequence
            workspace={root ?? ""}
            onReview={async (request, write) => {
              let result: CorrelationReviewResult = { state: "failed", reason: "The review did not run." };
              await operate("sequence", async () => {
                result = await (write ? decideCorrelation(request) : openCorrelationReview(request));
              });
              return result;
            }}
            rulesEntries={(opened?.artifacts ?? [])
              .filter((artifact) => artifact.kind === "rules")
              .map((artifact) => artifact.name)}
            analyses={(opened?.artifacts ?? [])
              .filter((artifact) => artifact.kind === "analysis")
              .map((artifact) => artifact.name)}
            reviews={(opened?.artifacts ?? [])
              .filter((artifact) => artifact.kind === "review")
              .map((artifact) => artifact.name)}
            result={sequenceResult}
            busy={busy}
            progress={running === "sequence" ? "Laying this case out as a sequence." : null}
            indicators={indicators}
            onOpen={(rules, offset, analysis) => void layOutSequence(rules, offset, analysis)}
            onSelect={(occurrence) => void inspect(occurrence, "", 0, -1)}
          />
        ) : null}
        {verified ? (
          <Comparison
            entries={(opened?.artifacts ?? [])
              .filter((artifact) => artifact.kind === "case")
              .map((artifact) => artifact.name)}
            result={comparisonResult}
            busy={busy}
            progress={running === "comparison" ? "Comparing these collections." : null}
            indicators={indicators}
            onCompare={(right, keys, fields, offset) =>
              void compareCollections(right, keys, fields, offset)
            }
          />
        ) : null}
        {verified ? (
          <RevisionComparison
            entries={(opened?.artifacts ?? []).map((artifact) => artifact.name)}
            result={revisionResult}
            busy={busy}
            progress={running === "revisions" ? "Comparing these revisions." : null}
            indicators={indicators}
            onCompare={(left, right, leftResult, rightResult) =>
              void compareRevisions(left, right, leftResult, rightResult)
            }
          />
        ) : null}
        {opened ? (
          <Review
            ruleEntries={(opened.artifacts ?? []).filter((artifact) => artifact.kind === "rules").map((artifact) => artifact.name)}
            planEntries={(opened.artifacts ?? []).filter((artifact) => artifact.kind === "plan").map((artifact) => artifact.name)}
            packEntries={(opened.artifacts ?? []).filter((artifact) => artifact.kind === "pack").map((artifact) => artifact.name)}
            reviewEntries={(opened.artifacts ?? []).filter((artifact) => artifact.kind === "review").map((artifact) => artifact.name)}
            transformResult={transformResult}
            reviewResult={reviewResult}
            caseOpen={verified !== null}
            busy={busy}
            transformProgress={
              running === "transformation" ? "Previewing this transformation." : null
            }
            reviewProgress={running === "review" ? "Reading this export review." : null}
            indicators={indicators}
            onPreview={(rules, plan, profile) => void previewPlan(rules, plan, profile)}
            onReview={(review, approve, offset) => void readReview(review, approve, offset)}
          />
        ) : null}
        {opened ? (
          <EnvironmentPanel
            workspace={root ?? ""}
            targetFile={environmentArtifact?.kind === "target" ? environmentArtifact.name : "targets/default.json"}
            secretsFile={environmentArtifact?.kind === "secrets" ? environmentArtifact.name : "secrets.json"}
            policyFile={environmentArtifact?.kind === "policy" ? environmentArtifact.name : "send-policy.json"}
            planFile={environmentArtifact?.kind === "reset" ? environmentArtifact.name : "reset-plan.json"}
            initialTab={environmentArtifact?.kind ?? "target"}
            drafts={drafts}
          />
        ) : null}
        {!evidence && !busy ? <p className="hint">Open a case to see what it holds.</p> : null}
      </>
    ),
    privacy: (
      <>
        <OperationAccess />
        <HubPanel />
        <p className="statement">{described?.privacy.statement}</p>
        <ul className="absent">
          {(described?.privacy.absent ?? []).map((claim) => (
            <li key={claim}>{claim}</li>
          ))}
        </ul>
        <h3>Kept on this machine</h3>
        <ul className="kept">
          {(described?.privacy.kept ?? []).map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      </>
    ),
  };

  if (!described) {
    return (
      <main className="starting">
        <h1>readmit</h1>
        <Status indicator={indicators.get(windowState)} state={windowState} reason={windowReason} />
      </main>
    );
  }

  const panes = {
    "--evidence-fraction": `${split}fr`,
    "--inspector-fraction": `${100 - split}fr`,
  } as CSSProperties;

  return (
    <>
      <main className="shell" style={panes}>
        {described.regions.map((region) => (
          <Fragment key={region.id}>
            <section
              className={`region region-${region.id}`}
              style={{ gridArea: region.id }}
              aria-labelledby={`${region.id}-heading`}
              tabIndex={-1}
              ref={(element) => {
                regionElements.current[region.id] = element;
              }}
              onFocus={() => setFocused(region.id)}
            >
              <h2 id={`${region.id}-heading`}>{region.label}</h2>
              <ContextHelp region={region.id} />
              {content[region.id]}
            </section>
            {region.id === "evidence" ? (
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
          </Fragment>
        ))}
      </main>

      <Palette
        open={paletteOpen}
        commands={listed}
        query={paletteQuery}
        onQuery={setPaletteQuery}
        onClose={() => setPaletteOpen(false)}
        onRun={(command) => latest.current[command]()}
      />
    </>
  );
}
