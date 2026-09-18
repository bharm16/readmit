import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import {
  cancel,
  createSampleWorkspace,
  openCase,
  openProject,
  openWorkspace,
  recentWorkspaces,
  search,
  selectWorkspace,
  shell as describeWindow,
  type CaseResult,
  type CommandId,
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

/** The panes never collapse to nothing: either one keeps a usable share of the
 * window, whether it is dragged or moved with a keyboard. */
const MIN_SPLIT = 20;
const MAX_SPLIT = 80;
const SPLIT_STEP = 5;

/** Verifying a case, reading a project and searching all run to completion once
 * they start, so Cancel is offered only while an interruptible operation runs. */
type Running = null | "workspace" | "case" | "project" | "search";

/** Every status carries its own word and its own shape. Colour is decoration on
 * top of both, never the difference between two of them. The shape is marked
 * decorative because the word beside it already says the same thing. */
function Status({
  indicator,
  state,
  reason,
}: {
  indicator: Indicator | undefined;
  state: State;
  reason?: string | undefined;
}) {
  return (
    <p className={`status status-${state}`} role="status">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      <span className="state">{indicator?.label ?? state}</span>
      {reason ? <span className="reason">{reason}</span> : null}
    </p>
  );
}

function Badge({ indicator, fallback }: { indicator: Indicator | undefined; fallback: string }) {
  return (
    <span className="badge">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      {indicator?.label ?? fallback}
    </span>
  );
}

export default function App() {
  const [described, setDescribed] = useState<Shell | null>(null);
  const [description, setDescription] = useState<State>("busy");
  const [descriptionReason, setDescriptionReason] = useState<string | undefined>(undefined);

  const [running, setRunning] = useState<Running>(null);
  const [workspace, setWorkspace] = useState<WorkspaceResult | null>(null);
  const [evidence, setEvidence] = useState<CaseResult | null>(null);
  const [investigation, setInvestigation] = useState<ProjectResult | null>(null);
  const [recent, setRecent] = useState<RecentResult | null>(null);
  const [found, setFound] = useState<SearchResult | null>(null);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const [focused, setFocused] = useState<RegionId>("commands");
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteQuery, setPaletteQuery] = useState("");
  const [theme, setTheme] = useState<Theme>("system");
  const [scale, setScale] = useState(0);
  const [split, setSplit] = useState(58);
  const [dragging, setDragging] = useState(false);

  const regionElements = useRef<Partial<Record<RegionId, HTMLElement | null>>>({});
  const searchField = useRef<HTMLInputElement | null>(null);
  const palette = useRef<HTMLDialogElement | null>(null);

  const indicators = useMemo(() => {
    const table = new Map<StatusValue, Indicator>();
    for (const indicator of described?.indicators ?? []) {
      table.set(indicator.status, indicator);
    }
    return table;
  }, [described]);

  useEffect(() => {
    void (async () => {
      const result = await describeWindow();
      setDescribed(result.shell ?? null);
      setDescription(result.state);
      setDescriptionReason(result.reason);
    })();
  }, []);

  const refreshRecent = useCallback(async () => {
    setRecent(await recentWorkspaces());
  }, []);

  useEffect(() => {
    void refreshRecent();
  }, [refreshRecent]);

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

  useEffect(() => {
    const dialog = palette.current;
    if (!dialog) {
      return;
    }
    if (paletteOpen && !dialog.open) {
      dialog.showModal();
    } else if (!paletteOpen && dialog.open) {
      dialog.close();
    }
  }, [paletteOpen]);

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

  const run = useCallback(
    async (operation: () => Promise<WorkspaceResult>) => {
      setRunning("workspace");
      setEvidence(null);
      setInvestigation(null);
      setFound(null);
      setSelected(null);
      setWorkspace(null);
      setWorkspace(await operation());
      setRunning(null);
      await refreshRecent();
      focusRegion("navigation");
    },
    [focusRegion, refreshRecent],
  );

  const inspect = useCallback(
    async (folder: string, name: string) => {
      setRunning("case");
      setEvidence(null);
      setSelected(name);
      setEvidence(await openCase(folder, name));
      setRunning(null);
      focusRegion("inspector");
    },
    [focusRegion],
  );

  const read = useCallback(
    async (folder: string) => {
      setRunning("project");
      setInvestigation(null);
      setInvestigation(await openProject(folder));
      setRunning(null);
      focusRegion("evidence");
    },
    [focusRegion],
  );

  const look = useCallback(
    async (folder: string, wanted: string) => {
      setRunning("search");
      setFound(null);
      setFound(await search(folder, wanted));
      setRunning(null);
    },
    [],
  );

  const resize = useCallback((delta: number) => {
    setSplit((current) => Math.min(MAX_SPLIT, Math.max(MIN_SPLIT, current + delta)));
  }, []);

  const busy = running !== null;

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
        void run(selectWorkspace);
      }
    },
    "create-sample-workspace": () => {
      if (!busy) {
        void run(createSampleWorkspace);
      }
    },
    "open-project": () => {
      if (!busy && root) {
        void read(root);
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
    "larger-text": () => setScale((current) => Math.min((described?.text_scales.length ?? 1) - 1, current + 1)),
    "smaller-text": () => setScale((current) => Math.max(0, current - 1)),
    "switch-theme": () => {
      const themes = described?.themes ?? ["system"];
      const next = themes[(themes.indexOf(theme) + 1) % themes.length];
      setTheme(next ?? "system");
    },
  };

  // The keyboard listener is registered once and reads the current commands, so
  // a shortcut never runs a stale action and the window never re-binds keys.
  const latest = useRef(actions);
  useEffect(() => {
    latest.current = actions;
  });

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const commanded = event.ctrlKey || event.metaKey;
      const chosen = ((): CommandId | null => {
        if (event.key === "F6") {
          return event.shiftKey ? "previous-region" : "next-region";
        }
        if (!commanded) {
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
  }, []);

  const opened = workspace?.workspace;
  const project = investigation?.project;
  const visible = (described?.commands ?? []).filter((command) => {
    const wanted = paletteQuery.trim().toLowerCase();
    return (
      wanted === "" ||
      command.title.toLowerCase().includes(wanted) ||
      (command.keys ?? "").toLowerCase().includes(wanted)
    );
  });

  const content: Record<RegionId, ReactNode> = {
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
          <select
            id="theme"
            value={theme}
            onChange={(event) => setTheme(event.target.value as Theme)}
          >
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
              void look(root, query);
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
        {running === "search" ? (
          <Status indicator={indicators.get("busy")} state="busy" reason="Searching this workspace." />
        ) : null}
        {found && found.state !== "completed" ? (
          <Status indicator={indicators.get(found.state)} state={found.state} reason={found.reason} />
        ) : null}
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
        {running === "workspace" ? (
          <Status indicator={indicators.get("busy")} state="busy" reason="Opening the folder." />
        ) : null}
        {workspace ? (
          <Status
            indicator={indicators.get(workspace.state)}
            state={workspace.state}
            reason={workspace.reason}
          />
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
                      onClick={() => void inspect(opened.root, artifact.name)}
                    >
                      Verify and open
                    </button>
                  </>
                ) : null}
                {artifact.kind === "project" ? (
                  <button type="button" disabled={busy} onClick={() => void read(opened.root)}>
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
              <button type="button" disabled={busy} onClick={() => void run(() => openWorkspace(folder))}>
                {folder}
              </button>
            </li>
          ))}
        </ul>
      </>
    ),
    evidence: (
      <>
        {running === "project" ? (
          <Status indicator={indicators.get("busy")} state="busy" reason="Reading the project." />
        ) : null}
        {investigation ? (
          <Status
            indicator={indicators.get(investigation.state)}
            state={investigation.state}
            reason={investigation.reason}
          />
        ) : null}
        {project ? (
          <>
            <h3>{project.settings.title}</h3>
            <p className="reason">
              {project.schema} · interface versions {project.interface_versions.join(", ")}
            </p>
            <ul className="registered">
              {project.cases.map((registered) => (
                <li
                  key={registered.name}
                  aria-current={selected === registered.name ? "true" : undefined}
                >
                  <span className="name">{registered.title}</span>
                  <Badge indicator={indicators.get(registered.status)} fallback={registered.status} />
                  <span className="badge">{registered.interface_version}</span>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => root && void inspect(root, registered.name)}
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
        {running === "case" ? (
          <Status indicator={indicators.get("busy")} state="busy" reason="Verifying the case." />
        ) : null}
        {evidence ? (
          <Status
            indicator={indicators.get(evidence.state)}
            state={evidence.state}
            reason={evidence.reason}
          />
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
        {!evidence && !busy ? <p className="hint">Open a case to see what it holds.</p> : null}
      </>
    ),
    privacy: described ? (
      <>
        <p className="statement">{described.privacy.statement}</p>
        <ul className="absent">
          {described.privacy.absent.map((claim) => (
            <li key={claim}>{claim}</li>
          ))}
        </ul>
        <h3>Kept on this machine</h3>
        <ul className="kept">
          {described.privacy.kept.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      </>
    ) : null,
  };

  if (!described) {
    return (
      <main className="starting">
        <h1>readmit</h1>
        <Status indicator={indicators.get(description)} state={description} reason={descriptionReason} />
      </main>
    );
  }

  const panes = { "--evidence-fraction": `${split}fr`, "--inspector-fraction": `${100 - split}fr` } as CSSProperties;

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
              <div
                className="separator"
                style={{ gridArea: "separator" }}
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize the evidence and inspector panes"
                aria-valuenow={split}
                aria-valuemin={MIN_SPLIT}
                aria-valuemax={MAX_SPLIT}
                tabIndex={0}
                onKeyDown={(event) => {
                  const moved =
                    event.key === "ArrowLeft"
                      ? -SPLIT_STEP
                      : event.key === "ArrowRight"
                        ? SPLIT_STEP
                        : 0;
                  if (moved !== 0) {
                    event.preventDefault();
                    resize(moved);
                  } else if (event.key === "Home" || event.key === "End") {
                    event.preventDefault();
                    setSplit(event.key === "Home" ? MIN_SPLIT : MAX_SPLIT);
                  }
                }}
                onPointerDown={(event) => {
                  event.currentTarget.setPointerCapture(event.pointerId);
                  setDragging(true);
                }}
                onPointerUp={(event) => {
                  event.currentTarget.releasePointerCapture(event.pointerId);
                  setDragging(false);
                }}
                onPointerMove={(event) => {
                  const left = regionElements.current.evidence?.getBoundingClientRect().left;
                  const right = regionElements.current.inspector?.getBoundingClientRect().right;
                  if (!dragging || left === undefined || right === undefined || right <= left) {
                    return;
                  }
                  const fraction = ((event.clientX - left) / (right - left)) * 100;
                  setSplit(Math.min(MAX_SPLIT, Math.max(MIN_SPLIT, Math.round(fraction))));
                }}
              />
            ) : null}
          </Fragment>
        ))}
      </main>

      <dialog
        className="palette"
        ref={palette}
        aria-label="Command palette"
        onClose={() => setPaletteOpen(false)}
      >
        <form
          method="dialog"
          onSubmit={(event) => {
            event.preventDefault();
            const first = visible[0];
            setPaletteOpen(false);
            first?.id && latest.current[first.id]();
          }}
        >
          <label htmlFor="palette-query">Type a command</label>
          <input
            id="palette-query"
            type="text"
            autoFocus
            value={paletteQuery}
            onChange={(event) => setPaletteQuery(event.target.value)}
          />
        </form>
        <ul aria-label="Commands">
          {visible.map((command) => (
            <li key={command.id}>
              <button
                type="button"
                onClick={() => {
                  setPaletteOpen(false);
                  latest.current[command.id]();
                }}
              >
                <span className="name">{command.title}</span>
                {command.keys ? <kbd>{command.keys}</kbd> : null}
              </button>
            </li>
          ))}
        </ul>
        <button type="button" onClick={() => setPaletteOpen(false)}>
          Close
        </button>
      </dialog>
    </>
  );
}
