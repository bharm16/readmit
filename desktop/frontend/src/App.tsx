import { NoteDraft } from "./NoteDraft";
import { Recovery } from "./Recovery";
import { RunPanel } from "./RunPanel";
import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactElement } from "react";
import {
  buildReproducer,
  cancel,
  createSampleWorkspace,
  editReproducer,
  undoReproducer,
  type ReproducerPlan,
  type ReproducerResult,
  type ReproducerStep,
  filters as readFilters,
  openCase,
  openGrid,
  inspectOccurrence,
  type InspectionResult,
  openProject,
  openWorkspace,
  recentWorkspaces,
  recordView,
  recoverSession,
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
  type ProjectResult,
  type RecentResult,
  type RegionId,
  type SearchResult,
  type Shell,
  type State,
  type StatusValue,
  type Theme,
  type WorkspaceResult,
} from "./bindings";
import { Inspector } from "./Inspector";
import { Reproducer } from "./Reproducer";
import { Badge, GRID_WINDOW, MessageGrid, Palette, Report, Separator, Status } from "./shell";

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
  | "reproducer";

export default function App() {
  const [described, setDescribed] = useState<Shell | null>(null);
  const [windowState, setWindowState] = useState<State>("busy");
  const [windowReason, setWindowReason] = useState<string | undefined>(undefined);

  const [running, setRunning] = useState<Running>(null);
  const [workspace, setWorkspace] = useState<WorkspaceResult | null>(null);
  const [evidence, setEvidence] = useState<CaseResult | null>(null);
  const [investigation, setInvestigation] = useState<ProjectResult | null>(null);
  const [recent, setRecent] = useState<RecentResult | null>(null);
  const [found, setFound] = useState<SearchResult | null>(null);
  const [savedFilters, setSavedFilters] = useState<FiltersResult | null>(null);
  const [inspectionResult, setInspectionResult] = useState<InspectionResult | null>(null);
  const [selectedOccurrence, setSelectedOccurrence] = useState<string | null>(null);
  const [gridResult, setGridResult] = useState<GridResult | null>(null);
  const [reproducerResult, setReproducerResult] = useState<ReproducerResult | null>(null);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const [restored, setRestored] = useState<RecoveryResult | null>(null);
  const [watchedRun, setWatchedRun] = useState("");

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

  // Where this viewer is, retained by the facade so an interruption does not
  // also lose it: the open workspace, the entry selected in it, the region
  // holding focus, and the run being watched. It goes into the facade's own
  // local document, never into browser storage and never near evidence.
  // Recording it does not claim the operation slot, so it still happens while a
  // case is being verified, which is exactly when an interruption would
  // otherwise lose the most. Nothing is recorded until there is something to
  // come back to: recording an empty view as the window starts would overwrite
  // the session this same window is restoring.
  const workspaceRoot = workspace?.workspace?.root ?? "";
  const view = useCallback((run: string) => ({
    workspace: workspaceRoot,
    region: focused,
    case: workspaceRoot === "" ? "" : (selected ?? ""),
    run,
  }), [workspaceRoot, focused, selected]);

  useEffect(() => {
    if (workspaceRoot === "" && watchedRun === "") {
      return;
    }
    void recordView(view(watchedRun));
  }, [view, watchedRun]);

  // Naming the run folder is the one recording that has to have landed before
  // the action it describes runs, so it is awaited rather than left to the
  // effect above: a crash during a send must find the session already naming
  // the folder that holds its evidence.
  const watch = useCallback(async (folder: string) => {
    setWatchedRun(folder);
    await recordView(view(folder));
  }, [view]);

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

  const root = workspace?.workspace?.root ?? null;

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

  const openFolder = useCallback(
    async (operation: () => Promise<WorkspaceResult>) => {
      await operate("workspace", async () => {
        setEvidence(null);
        setInvestigation(null);
        setFound(null);
        setGridResult(null);
        setInspectionResult(null);
        setReproducerResult(null);
        setSelectedOccurrence(null);
        setSelected(null);
        setWorkspace(null);
        setWorkspace(await operation());
      });
      await refreshRecent();
      focusRegion("navigation");
    },
    [focusRegion, operate, refreshRecent],
  );

  const verifyCase = useCallback(
    async (folder: string, name: string) => {
      await operate("case", async () => {
        setEvidence(null);
        setGridResult(null);
        setInspectionResult(null);
        setReproducerResult(null);
        setSelectedOccurrence(null);
        setSelected(name);
        setEvidence(await openCase(folder, name));
      });
      focusRegion("inspector");
    },
    [focusRegion, operate],
  );

  const readProject = useCallback(
    async (folder: string) => {
      await operate("project", async () => {
        setInvestigation(null);
        setInvestigation(await openProject(folder));
      });
      focusRegion("evidence");
    },
    [focusRegion, operate],
  );

  const searchWorkspace = useCallback(
    async (folder: string, wanted: string) => {
      await operate("search", async () => {
        setFound(null);
        setFound(await search(folder, wanted));
      });
    },
    [operate],
  );

  // One window of the grid at a time. Asking for the next one re-reads the case
  // and its index, so a window is never served from an index the evidence no
  // longer supports.
  const showGrid = useCallback(
    async (folder: string, name: string, indexName: string, offset: number) => {
      await operate("grid", async () => {
        setGridResult(null);
        setInspectionResult(null);
        setSelectedOccurrence(null);
        setGridResult(await openGrid(folder, name, indexName, offset, GRID_WINDOW));
      });
    },
    [operate],
  );

  const inspect = useCallback(
    async (occurrence: string, path: string, nodeOffset: number, byteOffset: number) => {
      const grid = gridResult?.grid;
      if (!root || !grid) return;
      await operate("inspect", async () => {
        setSelectedOccurrence(occurrence);
        setInspectionResult(null);
        setInspectionResult(
          await inspectOccurrence({
            workspace: root,
            case: grid.case,
            identity: grid.identity,
            occurrence,
            path,
            node_offset: nodeOffset,
            byte_offset: byteOffset,
          }),
        );
      });
    },
    [gridResult, operate, root],
  );

  // A reproducer plan is bound to the case the grid verified, so every step
  // carries the plan back to the engine, which decides what it means. The
  // window keeps no second copy of the selection or of what a dependency added.
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
      });
    },
    [gridResult, operate, reproducerResult, root],
  );

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

  const busy = running !== null;

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

  const opened = workspace?.workspace;
  const project = investigation?.project;
  const verified = evidence?.case ?? null;
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
              <li key={`${match.kind}:${match.name}:${match.field}`}>
                <button
                  type="button"
                  onClick={() => {
                    setSelected(match.name);
                    focusRegion(match.region);
                  }}
                >
                  <span className="name">{match.label}</span>
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
        <Report
          indicators={indicators}
          progress={running === "workspace" ? "Opening the folder." : null}
          result={workspace}
        />
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
        <Recovery restored={restored} onChanged={() => void restore()} />
        <NoteDraft project={workspaceRoot} restored={restored} onChanged={() => void restore()} />
        <RunPanel onWatch={watch} />
        <Report
          indicators={indicators}
          progress={running === "project" ? "Reading the project." : null}
          result={investigation}
        />
        {project ? (
          <>
            <h3>{project.settings.title}</h3>
            <p className="reason">
              {project.schema} · interface versions {project.interface_versions.join(", ")}
            </p>
            <ul className="registered">
              {project.cases.map((registered) => (
                <li key={registered.name} aria-current={selected === registered.name ? "true" : undefined}>
                  <span className="name">{registered.title}</span>
                  <Badge indicator={indicators.get(registered.status)} fallback={registered.status} />
                  <span className="badge">{registered.interface_version}</span>
                  <button
                    type="button"
                    disabled={busy || !root}
                    onClick={() => {
                      if (root) {
                        void verifyCase(root, registered.name);
                      }
                    }}
                  >
                    Verify and open
                  </button>
                </li>
              ))}
            </ul>
          </>
        ) : null}
        {!investigation && !busy ? (
          <p className="hint">
            Read a project document to see the cases it registers, or open a case from the listing.
          </p>
        ) : null}
      </>
    ),
    inspector: (
      <>
        <Report
          indicators={indicators}
          progress={running === "case" ? "Verifying the case." : null}
          result={evidence}
        />
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
              .filter((artifact) => artifact.kind === "unsupported")
              .map((artifact) => artifact.name)}
            busy={busy}
            onOpen={(indexName, offset) => {
              if (root) {
                void showGrid(root, verified.name, indexName, offset);
              }
            }}
            onSelect={(name) => void changeFilters(() => selectFilter(name))}
            onSave={(filter) => void changeFilters(() => saveFilter(filter))}
            selectedOccurrence={selectedOccurrence}
            onInspect={(occurrence) => void inspect(occurrence, "", 0, -1)}
          />
        ) : null}
        {gridResult?.grid ? (
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
        {!evidence && !busy ? <p className="hint">Open a case to see what it holds.</p> : null}
      </>
    ),
    privacy: (
      <>
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
