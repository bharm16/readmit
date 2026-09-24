import { StateHelp } from "./ContextHelp";
// The window furniture that renders the facade's description of the shell:
// how a status reads, the command palette, the pane separator, and the message
// grid. None of it decides anything about evidence; it draws what
// internal/desktop answered. The one thing the grid form settles here is what a
// browser date input cannot express — an entry that is not a complete date and
// time is refused rather than sent as no bound at all, because a time bound
// that silently disappeared would widen the filter. Whether a filter is usable
// is the facade's decision, and it reports it.
import { useEffect, useRef, useState } from "react";
import type {
  BuildIndexRequest,
  CaseEvidence,
  Command,
  CommandId,
  FieldMatch,
  FieldState,
  Filter,
  FiltersResult,
  GridResult,
  IndexDetails,
  IndexRetention,
  Indicator,
  OccurrenceKind,
  State,
  StatusValue,
} from "./bindings";

export type Indicators = Map<StatusValue, Indicator>;

/** Every status carries its own word and its own shape. Colour is decoration on
 * top of both, never the difference between two of them. The shape is marked
 * decorative because the word beside it already says the same thing, so an
 * assistive technology reads the word once and a missing glyph costs nothing. */
export function Status({
  indicator,
  state,
  reason,
}: {
  indicator: Indicator | undefined;
  state: State;
  reason?: string | undefined;
}) {
  return (
    <>
    <p className={`status status-${state}`} role="status">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      <span className="state">{indicator?.label ?? state}</span>
      {reason ? <span className="reason">{reason}</span> : null}
    </p>
    <StateHelp state={state} />
    </>
  );
}

export function Badge({
  indicator,
  fallback,
}: {
  indicator: Indicator | undefined;
  fallback: string;
}) {
  return (
    <span className="badge">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      {indicator?.label ?? fallback}
    </span>
  );
}

/** One region's outcome. While an operation is running that is the whole story,
 * so the previous outcome is not left on screen beside it. */
export function Report({
  indicators,
  progress,
  result,
}: {
  indicators: Indicators;
  progress: string | null;
  result: { state: State; reason?: string | undefined } | null;
}) {
  if (progress !== null) {
    return <Status indicator={indicators.get("busy")} state="busy" reason={progress} />;
  }
  if (!result) {
    return null;
  }
  return (
    <Status indicator={indicators.get(result.state)} state={result.state} reason={result.reason} />
  );
}

/** The separator between the evidence and inspector panes. It is in the tab
 * order and reports where it sits, so the panes resize with the arrow keys,
 * Home and End as well as with a pointer. */
export function Separator({
  split,
  min,
  max,
  step,
  onSplit,
  bounds,
}: {
  split: number;
  min: number;
  max: number;
  step: number;
  onSplit: (split: number) => void;
  bounds: () => { left: number; right: number } | null;
}) {
  const dragging = useRef(false);
  const clamp = (value: number) => Math.min(max, Math.max(min, value));
  return (
    <div
      className="separator"
      style={{ gridArea: "separator" }}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the evidence and inspector panes"
      aria-valuenow={split}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          onSplit(clamp(split + (event.key === "ArrowLeft" ? -step : step)));
        } else if (event.key === "Home" || event.key === "End") {
          event.preventDefault();
          onSplit(event.key === "Home" ? min : max);
        }
      }}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        dragging.current = true;
      }}
      onPointerUp={(event) => {
        event.currentTarget.releasePointerCapture(event.pointerId);
        dragging.current = false;
      }}
      onPointerMove={(event) => {
        const pane = bounds();
        if (!dragging.current || !pane || pane.right <= pane.left) {
          return;
        }
        const width = pane.right - pane.left;
        onSplit(clamp(Math.round(((event.clientX - pane.left) / width) * 100)));
      }}
    />
  );
}

/** The command palette. A native modal dialog traps focus and closes on Escape
 * without any of that being reimplemented here. */
export function Palette({
  open,
  commands,
  query,
  onQuery,
  onClose,
  onRun,
}: {
  open: boolean;
  commands: Command[];
  query: string;
  onQuery: (query: string) => void;
  onClose: () => void;
  onRun: (command: CommandId) => void;
}) {
  const dialog = useRef<HTMLDialogElement | null>(null);

  useEffect(() => {
    const element = dialog.current;
    if (!element) {
      return;
    }
    if (open && !element.open) {
      element.showModal();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  const choose = (command: CommandId | undefined) => {
    onClose();
    if (command) {
      onRun(command);
    }
  };

  return (
    <dialog className="palette" ref={dialog} aria-label="Command palette" onClose={onClose}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          choose(commands[0]?.id);
        }}
      >
        <label htmlFor="palette-query">Type a command</label>
        <input
          id="palette-query"
          type="text"
          autoFocus
          value={query}
          onChange={(event) => onQuery(event.target.value)}
        />
      </form>
      <ul aria-label="Commands">
        {commands.map((command) => (
          <li key={command.id}>
            <button type="button" onClick={() => choose(command.id)}>
              <span className="name">{command.title}</span>
              {command.keys ? <kbd>{command.keys}</kbd> : null}
            </button>
          </li>
        ))}
      </ul>
      <button type="button" onClick={onClose}>
        Close
      </button>
    </dialog>
  );
}

/** How many occurrences one window of the grid asks the facade for. The grid
 * asks for the next window rather than drawing a large case at once, and it is
 * the facade's own bound, because what this window costs to draw is decided by
 * the viewport below rather than by how many rows the window holds. */
export const GRID_WINDOW = 200;

/** The virtualized row geometry. GRID_ROW_HEIGHT is the height one row is
 * given, and it must stay equal to --grid-row-height in styles.css: how far the
 * rows have been scrolled is turned into a row number by dividing by it, so a
 * row drawn taller than this would drift away from the scrollbar.
 *
 * GRID_VIEWPORT_ROWS is how tall the scrolling viewport is, counted in rows and
 * including the caption and the column headers above them, so it shows fewer
 * than that many occurrences at once. GRID_OVERSCAN is how many rows are drawn on
 * either side of what is visible, so that a scroll does not reach the edge of
 * what has been drawn before the next render replaces it.
 *
 * Together they bound the rows in the document: a window of any size draws at
 * most GRID_VIEWPORT_ROWS + 2 * GRID_OVERSCAN of them, and a window no larger
 * than that is drawn whole. */
export const GRID_ROW_HEIGHT = 32;
export const GRID_VIEWPORT_ROWS = 16;
export const GRID_OVERSCAN = 8;

/** The rows of one window that a scroll position puts on screen, with the rows
 * before and after them left as measured space rather than as elements.
 * scrolled is how far the rows themselves have moved, which is the viewport's
 * own scroll position less the caption and headers above them. */
export function visibleRows(total: number, scrolled: number) {
  const drawn = GRID_VIEWPORT_ROWS + 2 * GRID_OVERSCAN;
  if (total <= drawn) {
    return { first: 0, last: total };
  }
  const centre = Math.floor(Math.max(0, scrolled) / GRID_ROW_HEIGHT);
  const first = Math.min(Math.max(0, centre - GRID_OVERSCAN), total - drawn);
  return { first, last: first + drawn };
}

/** The message grid: one window over one filtered case.
 *
 * It renders what the facade answered and decides nothing. Every counted fact
 * below comes from the facade, including how many occurrences the selected
 * filter removed from the view, which is shown whenever a grid is shown: a
 * filtered view that hides records without saying how many would read as though
 * the case held nothing else. Nothing read out of a message is here — a row
 * carries where an occurrence is and what it is. */
export const COMMON_INDEX_FIELDS = [
  { selector: "PID-3", label: "Patient Identifier (PID-3)" },
  { selector: "MSH-10", label: "Message Control ID (MSH-10)" },
  { selector: "MSA[1]-1[1]", label: "ACK Code (MSA-1)" },
  { selector: "PV1-19", label: "Visit Number (PV1-19)" },
  { selector: "MSH-9.1", label: "Message Code (MSH-9.1)" },
  { selector: "MSH-9.2", label: "Trigger Event (MSH-9.2)" },
  { selector: "EVN-2", label: "Recorded Date/Time (EVN-2)" },
];

export function MessageGrid({
  indicators,
  progress,
  result,
  filters,
  entries,
  busy,
  onOpen,
  onSelect,
  onSave,
  selectedOccurrence,
  onInspect,
  caseEvidence,
  indexDetails,
  onBuildIndex,
}: {
  indicators: Indicators;
  progress: string | null;
  result: GridResult | null;
  filters: FiltersResult | null;
  /** The entries of the open folder that are neither a case bundle nor one of
   * the project documents — an index is a file, so it is among these. Whether
   * one really is an index of this case is the facade's decision. */
  entries: string[];
  busy: boolean;
  onOpen: (indexName: string, offset: number) => void;
  onSelect: (name: string) => void;
  onSave: (filter: Filter) => void;
  selectedOccurrence: string | null;
  onInspect: (occurrence: string) => void;
  caseEvidence?: CaseEvidence | null | undefined;
  indexDetails?: IndexDetails | null | undefined;
  onBuildIndex?: ((request: BuildIndexRequest) => void) | undefined;
}) {
  const [indexName, setIndexName] = useState("");
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [invalid, setInvalid] = useState<string | null>(null);
  const [scrolled, setScrolled] = useState(0);
  const [showBuildForm, setShowBuildForm] = useState(false);
  const [buildFields, setBuildFields] = useState<string[]>(["PID-3", "MSH-10"]);
  const [customField, setCustomField] = useState("");
  const [retention, setRetention] = useState<IndexRetention>("states");
  const [retainUntil, setRetainUntil] = useState("");
  const [isIndefinite, setIsIndefinite] = useState(true);
  const [outputName, setOutputName] = useState("");
  const [replaceExisting, setReplaceExisting] = useState(false);

  const viewport = useRef<HTMLDivElement | null>(null);
  const body = useRef<HTMLTableSectionElement | null>(null);
  const filterName = useRef<HTMLInputElement | null>(null);
  const grid = result?.grid ?? null;
  const ownedIndex = Boolean(caseEvidence && indexDetails?.identity && indexDetails.identity === caseEvidence.identity);
  const foreignIndex = Boolean(caseEvidence && indexDetails?.identity && indexDetails.identity !== caseEvidence.identity);
  const unownedIndex = Boolean(indexDetails && !indexDetails.applicable && !ownedIndex);
  const rebuildableIndex = Boolean(indexDetails && !indexDetails.applicable && ownedIndex);
  const freshIndexName = (name: string) => {
    const standard = `${name}.index.json`;
    if (!entries.includes(standard)) return standard;
    // A changed case can keep the same directory name while its old index
    // remains in the workspace. Suggest an unused entry, never replacement.
    for (let suffix = 2; suffix <= entries.length + 2; suffix++) {
      const candidate = `${name}.${suffix}.index.json`;
      if (!entries.includes(candidate)) return candidate;
    }
    return standard;
  };

  // Discarding an unsaved filter writes nothing and changes no selection: the
  // form is emptied and the person is back at its first field.
  const discard = () => {
    setDraft(emptyDraft);
    setInvalid(null);
    filterName.current?.focus();
  };

  const caseName = caseEvidence?.name;
  const caseIdentity = caseEvidence?.identity;
  useEffect(() => {
    // A draft belongs to the case that opened it. In particular, a replacement
    // choice must never carry into a different case in the same workspace.
    setIndexName("");
    setShowBuildForm(false);
    setOutputName(caseName ? freshIndexName(caseName) : "");
    setReplaceExisting(false);
  }, [caseName, caseIdentity]);

  // A new window is a new list, so it starts at its own first row rather than
  // wherever the previous one had been scrolled to.
  useEffect(() => {
    setScrolled(0);
    if (viewport.current) {
      viewport.current.scrollTop = 0;
    }
  }, [grid?.index, grid?.offset, grid?.filter]);
  const rows = grid?.rows ?? [];
  const { first, last } = visibleRows(rows.length, scrolled);

  const openRebuild = () => {
    if (indexDetails?.fields && indexDetails.fields.length > 0) {
      setBuildFields(indexDetails.fields);
    }
    if (indexDetails?.retention) {
      setRetention((indexDetails.retention as IndexRetention) || "states");
    }
    if (indexDetails?.index_name) {
      setOutputName(indexDetails.index_name);
    } else if (caseEvidence) {
      setOutputName(`${caseEvidence.name}.index.json`);
    }
    setReplaceExisting(true);
    setShowBuildForm(true);
  };

  const openNewBuild = () => {
    setOutputName(caseEvidence ? freshIndexName(caseEvidence.name) : "");
    setReplaceExisting(false);
    setShowBuildForm(true);
  };

  return (
    <section className="grid" aria-label="Message grid">
      <h3>Message grid</h3>
      <div className="grid-open">
        <label htmlFor="grid-index">Index file in this folder</label>
        <select
          id="grid-index"
          value={indexName}
          disabled={busy || entries.length === 0}
          onChange={(event) => setIndexName(event.target.value)}
        >
          <option value="">Choose an index of this case…</option>
          {entries.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>
        <button type="button" disabled={busy || indexName === ""} onClick={() => onOpen(indexName, 0)}>
          Open the grid
        </button>
        {onBuildIndex && (
          <button
            type="button"
            className="action-open-build"
            disabled={busy}
            onClick={() => {
              if (rebuildableIndex) {
                openRebuild();
              } else if (unownedIndex) {
                openNewBuild();
              } else {
                setShowBuildForm((prev) => !prev);
              }
            }}
          >
            {showBuildForm
              ? "Close build form"
              : "Build index"}
          </button>
        )}
      </div>

      {foreignIndex ? (
        <div className="index-rebuild-banner" role="alert" aria-label="Index mismatch notice">
          <span className="warning-badge">[Index belongs to different evidence]</span>
          <p className="rebuild-reason">
            This index describes another case and cannot be used for the open case. Build a separate index for this case.
          </p>
        </div>
      ) : null}

      {unownedIndex && !foreignIndex ? (
        <div className="index-rebuild-banner" role="alert" aria-label="Index ownership unknown notice">
          <span className="warning-badge">[Index cannot be verified]</span>
          <p className="rebuild-reason">
            This index cannot be confirmed as belonging to the open case. Build a separate index for this case.
          </p>
        </div>
      ) : null}

      {rebuildableIndex ? (
        <div className="index-rebuild-banner" role="alert" aria-label="Index rebuild notice">
          <span className="warning-badge">[Index rebuild required]</span>
          <p className="rebuild-reason">
            {indexDetails?.stale && "The index was built from different evidence or is stale for this case."}
            {indexDetails?.expired && "The retention period declared for this index has ended."}
            {indexDetails?.damaged && "This index file does not match what was written for it."}
            {indexDetails?.unsupported && "This index was written under an unsupported version."}
            {" "}The case evidence is unchanged. Rebuild the index from this case to explore records.
          </p>
          {!showBuildForm && onBuildIndex ? (
            <button
              type="button"
              className="action-rebuild-index"
              disabled={busy}
              onClick={openRebuild}
            >
              Rebuild index
            </button>
          ) : null}
        </div>
      ) : null}

      {!grid && (!indexDetails || unownedIndex) && caseEvidence ? (
        <div className="unindexed-case" aria-label="Unindexed case">
          <h4>Case is unindexed</h4>
          <p className="hint">
            This case contains <strong>{caseEvidence.occurrences}</strong> occurrences ({caseEvidence.messages} messages, {caseEvidence.acknowledgements} ACKs, {caseEvidence.unparsed} unparsed) across <strong>{caseEvidence.sources}</strong> source{caseEvidence.sources === 1 ? "" : "s"}.
          </p>
          <p className="notice">
            The case is not empty. Wire bytes, headers, and decoded segments can be inspected directly in the Inspector below without an index. Build an index to enable search, filter matching, and paged row navigation.
          </p>
          {!showBuildForm && onBuildIndex ? (
            <button
              type="button"
              className="action-build-index"
              disabled={busy}
              onClick={openNewBuild}
            >
              Build case index
            </button>
          ) : null}
        </div>
      ) : null}

      {indexDetails && indexDetails.applicable ? (
        <div className="active-index-details" aria-label="Active index details">
          <div className="index-meta">
            <span className="index-chip">Index: {indexDetails.index_name}</span>
            <span className="retention-chip">Retention: {indexDetails.retention} ({indexDetails.retention_state})</span>
            <span className="expiry-chip">Retain until: {indexDetails.retain_until || "indefinite"}</span>
            <span className="records-chip">
              {indexDetails.records} records ({indexDetails.decoded} decoded, {indexDetails.undecodable} undecodable)
            </span>
          </div>
          <p className="index-fields">
            Indexed fields: <code>{indexDetails.fields.join(", ")}</code>
          </p>
          <p className="permitted-searches">
            {indexDetails.retention === "values" && "Permitted searches: full substring search, equality, presence, and absence"}
            {indexDetails.retention === "digests" && "Permitted searches: exact digest match, presence, and absence"}
            {indexDetails.retention === "states" && "Permitted searches: presence and absence checks only"}
          </p>
        </div>
      ) : null}

      {showBuildForm && onBuildIndex ? (
        <form
          className="build-index-form"
          aria-label="Build index form"
          onSubmit={(e) => {
            e.preventDefault();
            if (!caseEvidence || buildFields.length === 0 || !outputName.trim()) return;
            onBuildIndex({
              workspace: "",
              case: caseEvidence.name,
              identity: caseEvidence.identity,
              output: outputName.trim(),
              fields: buildFields,
              retention,
              retain_until: isIndefinite
                ? "indefinite"
                : retainUntil
                  ? new Date(retainUntil).toISOString()
                  : "indefinite",
              replace: rebuildableIndex && replaceExisting,
            });
            setShowBuildForm(false);
          }}
        >
          <h4>{rebuildableIndex ? "Rebuild case index" : "Build case index"}</h4>

          <fieldset className="field-selection">
            <legend>Indexed fields ({buildFields.length} of 16 selected)</legend>
            <div className="common-fields">
              {COMMON_INDEX_FIELDS.map(({ selector, label }) => {
                const checked = buildFields.includes(selector);
                return (
                  <label key={selector} className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={busy || (!checked && buildFields.length >= 16)}
                      onChange={(e) => {
                        if (e.target.checked) {
                          if (buildFields.length < 16) setBuildFields([...buildFields, selector]);
                        } else {
                          setBuildFields(buildFields.filter((f) => f !== selector));
                        }
                      }}
                    />
                    {label}
                  </label>
                );
              })}
            </div>
            <div className="custom-field">
              <input
                type="text"
                placeholder="Custom selector (e.g. OBX[1]-3)"
                aria-label="Custom field selector"
                value={customField}
                disabled={busy || buildFields.length >= 16}
                onChange={(e) => setCustomField(e.target.value)}
              />
              <button
                type="button"
                disabled={
                  busy ||
                  !customField.trim() ||
                  buildFields.includes(customField.trim()) ||
                  buildFields.length >= 16
                }
                onClick={() => {
                  const trimmed = customField.trim();
                  if (trimmed && !buildFields.includes(trimmed) && buildFields.length < 16) {
                    setBuildFields([...buildFields, trimmed]);
                    setCustomField("");
                  }
                }}
              >
                Add field
              </button>
            </div>
            <div className="selected-fields-list">
              {buildFields.map((field) => (
                <span key={field} className="field-tag">
                  <code>{field}</code>
                  <button
                    type="button"
                    aria-label={`Remove field ${field}`}
                    disabled={busy}
                    onClick={() => setBuildFields(buildFields.filter((f) => f !== field))}
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          </fieldset>

          <fieldset className="retention-selection">
            <legend>Retention form</legend>
            <label className="radio-label">
              <input
                type="radio"
                name="retention"
                value="states"
                checked={retention === "states"}
                disabled={busy}
                onChange={() => setRetention("states")}
              />
              <span>
                <strong>States only (Least privilege)</strong> — Records presence, absence, and byte spans. Zero clinical values or digests stored. Permitted queries: presence/absence checks.
              </span>
            </label>
            <label className="radio-label">
              <input
                type="radio"
                name="retention"
                value="digests"
                checked={retention === "digests"}
                disabled={busy}
                onChange={() => setRetention("digests")}
              />
              <span>
                <strong>Cryptographic digests</strong> — Records SHA-256 hashes. Enables exact-match equality queries without storing plaintext. Substring queries not permitted.
              </span>
            </label>
            <label className="radio-label">
              <input
                type="radio"
                name="retention"
                value="values"
                checked={retention === "values"}
                disabled={busy}
                onChange={() => setRetention("values")}
              />
              <span>
                <strong>Plaintext values</strong> — Stores decoded string values (up to 4096 bytes). Enables full substring and text searches. Carries PHI exposure risk.
              </span>
            </label>
          </fieldset>

          <fieldset className="expiry-selection">
            <legend>Retention duration</legend>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={isIndefinite}
                disabled={busy}
                onChange={(e) => setIsIndefinite(e.target.checked)}
              />
              Deliberate indefinite retention
            </label>
            {!isIndefinite ? (
              <div className="expiry-picker">
                <label htmlFor="retain-until-input">Retain until (UTC)</label>
                <input
                  id="retain-until-input"
                  type="datetime-local"
                  value={retainUntil}
                  disabled={busy}
                  onChange={(e) => setRetainUntil(e.target.value)}
                />
              </div>
            ) : null}
          </fieldset>

          <div className="output-selection">
            <label htmlFor="index-output-file">Output index file</label>
            <input
              id="index-output-file"
              type="text"
              value={outputName}
              disabled={busy}
              onChange={(e) => setOutputName(e.target.value)}
            />
            {rebuildableIndex ? (
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={replaceExisting}
                  disabled={busy}
                  onChange={(e) => setReplaceExisting(e.target.checked)}
                />
                Replace existing file if present
              </label>
            ) : null}
          </div>

          <div className="form-actions">
            <button
              type="submit"
              disabled={
                busy ||
                buildFields.length === 0 ||
                !outputName.trim() ||
                (!isIndefinite && !retainUntil)
              }
            >
              {rebuildableIndex ? "Rebuild index" : "Build index"}
            </button>
            <button type="button" disabled={busy} onClick={() => setShowBuildForm(false)}>
              Cancel
            </button>
          </div>
        </form>
      ) : null}

      <div className="grid-filter">
        <label htmlFor="grid-filter">Saved filter</label>
        <select
          id="grid-filter"
          value={filters?.selected ?? ""}
          disabled={busy}
          onChange={(event) => onSelect(event.target.value)}
        >
          <option value="">No filter — show every occurrence</option>
          {(filters?.filters ?? []).map((saved) => (
            <option key={saved.name} value={saved.name}>
              {saved.name}
            </option>
          ))}
        </select>
      </div>
      {filters && filters.state !== "completed" ? (
        <Status indicator={indicators.get(filters.state)} state={filters.state} reason={filters.reason} />
      ) : null}

      <Report indicators={indicators} progress={progress} result={result} />

      {grid ? (
        <>
          <p className="counts">
            <span>
              Showing {grid.rows.length} of {grid.matched} matching
            </span>
            <span className="excluded">
              {grid.excluded} of {grid.total} excluded by{" "}
              {grid.filter === "" ? "no filter" : grid.filter}
            </span>
            <span>{grid.undecided} values the index could not settle</span>
            <span>{grid.undecodable} the case could not decode</span>
          </p>
          <div className="grid-window">
            <button
              type="button"
              disabled={busy || grid.offset === 0}
              onClick={() => onOpen(grid.index, Math.max(0, grid.offset - grid.limit))}
            >
              Previous {grid.limit}
            </button>
            <span>
              Occurrences {grid.rows.length === 0 ? grid.offset : grid.offset + 1}–{grid.offset + grid.rows.length}
            </span>
            <button
              type="button"
              disabled={busy || grid.offset + grid.rows.length >= grid.matched}
              onClick={() => onOpen(grid.index, grid.offset + grid.limit)}
            >
              Next {grid.limit}
            </button>
          </div>
          <div
            className="grid-scroll"
            ref={viewport}
            style={{ maxHeight: `${GRID_ROW_HEIGHT * GRID_VIEWPORT_ROWS}px` }}
            onScroll={(event) =>
              // The caption and the column headers scroll with the rows, so how
              // far the rows have moved is the viewport's scroll position less
              // where the rows begin inside it. Measuring that rather than
              // assuming it keeps the drawn window on the row the scrollbar is
              // actually pointing at.
              setScrolled(event.currentTarget.scrollTop - (body.current?.offsetTop ?? 0))
            }
          >
            <table className="rows" aria-rowcount={rows.length + 1}>
              <caption>
                {grid.case} · {grid.index} · verified {grid.identity}
              </caption>
              <thead>
                <tr aria-rowindex={1}>
                  <th scope="col">Occurrence</th>
                  <th scope="col">Source</th>
                  <th scope="col">Type</th>
                  <th scope="col">Direction</th>
                  <th scope="col">Observed</th>
                  <th scope="col">Bytes</th>
                </tr>
              </thead>
              <tbody ref={body}>
                {first > 0 ? (
                  <tr className="spacer" aria-hidden="true">
                    <td colSpan={6} style={{ height: `${first * GRID_ROW_HEIGHT}px` }} />
                  </tr>
                ) : null}
                {rows.slice(first, last).map((row, index) => (
                  <tr
                    key={row.id}
                    aria-rowindex={first + index + 2}
                    aria-selected={selectedOccurrence === row.id}
                  >
                    <th scope="row">
                      <button
                        type="button"
                        disabled={busy}
                        aria-pressed={selectedOccurrence === row.id}
                        onClick={() => onInspect(row.id)}
                      >
                        Inspect {row.id}
                      </button>
                    </th>
                    <td>{row.source_id}</td>
                    <td>
                      <span className="kind">{row.kind}</span>
                      {row.decoded ? null : <span className="unsupported">not decoded</span>}
                    </td>
                    <td>{row.direction}</td>
                    <td>{row.observed_at ?? "not recorded"}</td>
                    <td>
                      {row.offset}+{row.size}
                    </td>
                  </tr>
                ))}
                {last < rows.length ? (
                  <tr className="spacer" aria-hidden="true">
                    <td colSpan={6} style={{ height: `${(rows.length - last) * GRID_ROW_HEIGHT}px` }} />
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </>
      ) : null}

      <form
        className="grid-save"
        aria-label="Save a filter"
        onKeyDown={(event) => {
          // Escape discards the unsaved filter and goes no further: the
          // window's own Escape cancels a running operation.
          if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
            event.preventDefault();
            event.stopPropagation();
            discard();
          }
        }}
        onSubmit={(event) => {
          event.preventDefault();
          const filter = compose(draft);
          if (!filter) {
            setInvalid("A time bound has to be a complete date and time.");
            return;
          }
          setInvalid(null);
          onSave(filter);
        }}
      >
        <h4>Save a filter</h4>
        <label htmlFor="filter-name">Name</label>
        <input
          id="filter-name"
          ref={filterName}
          type="text"
          value={draft.name}
          onChange={(event) => setDraft({ ...draft, name: event.target.value })}
        />

        <span id="filter-kinds-label">Occurrence type</span>
        <div className="kinds" role="group" aria-labelledby="filter-kinds-label">
          {(["message", "ack", "unparsed"] as OccurrenceKind[]).map((kind) => (
            <label key={kind} htmlFor={`filter-kind-${kind}`}>
              <input
                id={`filter-kind-${kind}`}
                type="checkbox"
                checked={draft.kinds.includes(kind)}
                onChange={(event) =>
                  setDraft({
                    ...draft,
                    kinds: event.target.checked
                      ? [...draft.kinds, kind]
                      : draft.kinds.filter((chosen) => chosen !== kind),
                  })
                }
              />
              {kind}
            </label>
          ))}
        </div>

        <label htmlFor="filter-source">Source</label>
        <input
          id="filter-source"
          type="text"
          placeholder="s0001"
          value={draft.source}
          onChange={(event) => setDraft({ ...draft, source: event.target.value })}
        />

        <label htmlFor="filter-from">Observed from</label>
        <input
          id="filter-from"
          type="datetime-local"
          value={draft.from}
          onChange={(event) => setDraft({ ...draft, from: event.target.value })}
        />
        <label htmlFor="filter-until">Observed before</label>
        <input
          id="filter-until"
          type="datetime-local"
          value={draft.until}
          onChange={(event) => setDraft({ ...draft, until: event.target.value })}
        />

        <label htmlFor="filter-ack">ACK outcomes</label>
        <input
          id="filter-ack"
          type="text"
          placeholder="AA, AE, AR"
          value={draft.ackCodes}
          onChange={(event) => setDraft({ ...draft, ackCodes: event.target.value })}
        />

        <label htmlFor="filter-selector">Field</label>
        <input
          id="filter-selector"
          type="text"
          placeholder="PID[1]-3[1]"
          value={draft.selector}
          onChange={(event) => setDraft({ ...draft, selector: event.target.value })}
        />
        <label htmlFor="filter-match">Match</label>
        <select
          id="filter-match"
          value={draft.match}
          onChange={(event) => setDraft({ ...draft, match: event.target.value as FieldMatch })}
        >
          <option value="contains">contains</option>
          <option value="equals">equals</option>
          <option value="state">state</option>
        </select>
        {draft.match === "state" ? (
          <>
            <label htmlFor="filter-state">Decoded state</label>
            <select
              id="filter-state"
              value={draft.state}
              onChange={(event) => setDraft({ ...draft, state: event.target.value as FieldState })}
            >
              <option value="present">present</option>
              <option value="empty">empty</option>
              <option value="null">null</option>
              <option value="omitted">omitted</option>
            </select>
          </>
        ) : (
          <>
            <label htmlFor="filter-term">Value</label>
            <input
              id="filter-term"
              type="text"
              value={draft.term}
              onChange={(event) => setDraft({ ...draft, term: event.target.value })}
            />
          </>
        )}

        <button type="submit" disabled={busy}>
          Save and select
        </button>
        <button type="button" disabled={busy} onClick={discard}>
          Discard the unsaved filter
        </button>
        {invalid ? <p className="unsupported">{invalid}</p> : null}
        <p className="hint">
          A saved filter is kept on this machine, with whatever you typed to filter by. It is never
          written into evidence and never leaves this computer.
        </p>
      </form>
    </section>
  );
}

/** The authoring form's own state. It is turned into one Filter on submit, so
 * the typed contract is built in one place and never half-filled. */
type Draft = {
  name: string;
  kinds: OccurrenceKind[];
  source: string;
  from: string;
  until: string;
  ackCodes: string;
  selector: string;
  match: FieldMatch;
  term: string;
  state: FieldState;
};

const emptyDraft: Draft = {
  name: "",
  kinds: [],
  source: "",
  from: "",
  until: "",
  ackCodes: "",
  selector: "",
  match: "contains",
  term: "",
  state: "present",
};

/** instant turns one local date and time into the UTC instant the facade
 * stores. An entry that is not a complete date and time is refused rather than
 * dropped: a time bound that silently disappeared would widen the filter. */
function instant(value: string): string | null | undefined {
  if (value === "") {
    return null;
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString();
}

function compose(draft: Draft): Filter | null {
  const from = instant(draft.from);
  const until = instant(draft.until);
  if (from === undefined || until === undefined) {
    return null;
  }
  const asked = draft.selector.trim() !== "";
  return {
    name: draft.name.trim(),
    kinds: draft.kinds,
    sources: draft.source.trim() === "" ? [] : [draft.source.trim()],
    observed_from: from,
    observed_until: until,
    ack_codes: draft.ackCodes
      .split(",")
      .map((code) => code.trim())
      .filter((code) => code !== ""),
    fields: asked
      ? [
          {
            selector: draft.selector.trim(),
            match: draft.match,
            term: draft.match === "state" ? "" : draft.term,
            state: draft.match === "state" ? draft.state : "",
          },
        ]
      : [],
  };
}
