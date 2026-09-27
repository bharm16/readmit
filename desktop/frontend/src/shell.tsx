import { StateHelp } from "./ContextHelp";
import { IconButton } from "./IconButton";
// The window furniture that renders the facade's description of the shell:
// how a status reads, the command palette, the pane separator, and the message
// grid. None of it decides anything about evidence; it draws what
// internal/desktop answered. The one thing the grid form settles here is what a
// browser date input cannot express — an entry that is not a complete date and
// time is refused rather than sent as no bound at all, because a time bound
// that silently disappeared would widen the filter. Whether a filter is usable
// is the facade's decision, and it reports it.
import { useEffect, useId, useRef } from "react";
import type {
  BuildIndexRequest,
  IndexDetails,
  IndexRetention,
  Indicator,
  OccurrenceKind,
  State,
} from "./bindings";

/** Each indicator by the status it names. Go names a status as a string, and a
 * status without an indicator reads as its plain word. */
export type Indicators = Map<string, Indicator>;

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
    {/* What to do next matters only when something did not complete. */}
    {state === "failed" || state === "permission_denied" || state === "cancelled" ? <StateHelp state={state} /> : null}
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

/** The separator between the list and the details beside it. It is in the tab
 * order and reports the details' width in rem, so it resizes with the arrow
 * keys, Home and End as well as with a pointer. Moving it left widens the
 * details. */
export function Separator({
  value,
  min,
  max,
  step,
  onChange,
  edge,
  rem,
}: {
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (width: number) => void;
  /** Where the details end, in CSS pixels from the left of the window. */
  edge: () => number | null;
  /** The root text size, in CSS pixels. */
  rem: () => number;
}) {
  const dragging = useRef(false);
  const clamp = (width: number) => Math.min(max, Math.max(min, width));
  return (
    <div
      className="separator"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize details"
      aria-valuenow={Math.round(value * 10) / 10}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          onChange(clamp(value + (event.key === "ArrowLeft" ? step : -step)));
        } else if (event.key === "Home" || event.key === "End") {
          event.preventDefault();
          onChange(event.key === "Home" ? max : min);
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
        const right = edge();
        if (!dragging.current || right === null) return;
        onChange(clamp(Math.round(((right - event.clientX) / rem()) * 4) / 4));
      }}
    />
  );
}

/** The common HL7 fields an index can be asked to retain, captioned for a
 * person and sent as their exact selectors. */
export const COMMON_INDEX_FIELDS = [
  { selector: "PID-3", label: "Patient identifier (PID-3)" },
  { selector: "MSH-10", label: "Message control ID (MSH-10)" },
  { selector: "MSA[1]-1[1]", label: "ACK code (MSA-1)" },
  { selector: "PV1-19", label: "Visit number (PV1-19)" },
  { selector: "MSH-9.1", label: "Message code (MSH-9.1)" },
  { selector: "MSH-9.2", label: "Trigger event (MSH-9.2)" },
  { selector: "EVN-2", label: "Recorded date/time (EVN-2)" },
];

/** How many fields one index may retain, as `readmit index build` allows. */
const INDEX_FIELD_LIMIT = 16;

/** The unsaved definition of one index: what the setup view shows and what
 * Build index or Rebuild index sends. It is only ever written by one of those
 * two actions; opening, closing or prefilling the setup view writes nothing. */
export type IndexDraft = {
  fields: string[];
  custom: string;
  retention: IndexRetention;
  /** The retention end is "indefinite" only when this is chosen explicitly. */
  indefinite: boolean;
  /** A local date and time, as a datetime-local input holds it. */
  retainUntil: string;
  output: string;
  replace: boolean;
};

/** A new index definition, with the defaults the builder has always offered. */
export function newIndexDraft(output: string): IndexDraft {
  return {
    fields: ["PID-3", "MSH-10"],
    custom: "",
    retention: "states",
    indefinite: true,
    retainUntil: "",
    output,
    replace: false,
  };
}

/** The local date and time a datetime-local input shows for one instant, so a
 * deadline read back from an index is offered as the same instant it stored. */
function localInput(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }
  const pad = (n: number) => String(n).padStart(2, "0");
  const minutes = `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())}T${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
  return parsed.getSeconds() === 0 ? minutes : `${minutes}:${pad(parsed.getSeconds())}`;
}

/** A rebuild of the described index: its fields, stored representation,
 * retention end and file, so rebuilding keeps the policy it was built under.
 * A finite deadline stays finite; an index with none was built indefinitely. */
export function rebuildIndexDraft(details: IndexDetails, draft: IndexDraft): IndexDraft {
  const until = details.retain_until ? localInput(details.retain_until) : "";
  return {
    ...draft,
    fields: details.fields.length > 0 ? [...details.fields] : draft.fields,
    retention: details.retention || draft.retention,
    indefinite: !details.retain_until,
    retainUntil: until,
    output: details.index_name,
    replace: true,
  };
}

/** The shared index setup: the one form that declares a new index or the
 * rebuild of a verified one, used by the explorer and by maintenance. Build
 * index and Rebuild index are its only writes. Replacement is offered only for
 * replaceTarget — the selected index the caller verified as this case's — and
 * only applies while the output still names that exact file. */
export function IndexSetup({
  mode,
  draft,
  onDraft,
  caseName,
  identity,
  replaceTarget,
  busy,
  now,
  onBuild,
  onClose,
}: {
  mode: "build" | "rebuild";
  draft: IndexDraft;
  onDraft: (draft: IndexDraft) => void;
  caseName: string;
  identity: string;
  replaceTarget: string | null;
  busy: boolean;
  /** The current time, in milliseconds since the epoch, that a deadline must
   * lie after. */
  now: number;
  onBuild: (request: BuildIndexRequest) => void;
  onClose: () => void;
}) {
  const id = useId();
  const fieldList = useRef<HTMLDivElement | null>(null);
  const customInput = useRef<HTMLInputElement | null>(null);
  // Where focus goes once a removed field's control has left the page: the
  // field that took its place, or the field selector when none is left.
  const refocus = useRef<number | null>(null);
  useEffect(() => {
    const at = refocus.current;
    if (at === null) return;
    refocus.current = null;
    const remaining = fieldList.current?.querySelectorAll<HTMLButtonElement>("button") ?? [];
    const target = remaining[Math.min(at, remaining.length - 1)];
    (target ?? customInput.current)?.focus();
  }, [draft.fields]);

  const rebuilding = mode === "rebuild" && replaceTarget !== null;
  const until = draft.indefinite ? null : instant(draft.retainUntil);
  // A deadline that is not in the future would write an index already past
  // its retention, so it blocks the write like an incomplete one; a rebuild
  // keeps the ended deadline on screen rather than turning it indefinite.
  const passed = typeof until === "string" && Date.parse(until) <= now;
  const deadlineReady = draft.indefinite || (typeof until === "string" && !passed);
  const output = draft.output.trim();
  // Only the verified index's own file name is replaced; another name writes
  // a new index, and the action says which it is.
  const replacing = rebuilding && output === replaceTarget;
  const set = (change: Partial<IndexDraft>) => onDraft({ ...draft, ...change });
  const custom = draft.custom.trim();

  return (
    <form
      className="build-index-form dialog-form"
      aria-label="Build index form"
      onSubmit={(e) => {
        e.preventDefault();
        if (draft.fields.length === 0 || !output || !deadlineReady) return;
        onBuild({
          workspace: "",
          case: caseName,
          identity,
          output,
          fields: draft.fields,
          retention: draft.retention,
          // An incomplete local time never becomes indefinite retention: the
          // write is blocked above until the entry is a complete instant.
          retain_until: draft.indefinite ? "indefinite" : (until as string),
          replace: rebuilding && draft.replace && output === replaceTarget,
        });
      }}
    >

      <fieldset className="field-selection">
        <legend>Fields ({draft.fields.length} of 16)</legend>
        <div className="common-fields">
          {COMMON_INDEX_FIELDS.map(({ selector, label }) => {
            const checked = draft.fields.includes(selector);
            return (
              <label key={selector} className="checkbox-label">
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={busy || (!checked && draft.fields.length >= INDEX_FIELD_LIMIT)}
                  onChange={(e) => {
                    if (e.target.checked) {
                      if (draft.fields.length < INDEX_FIELD_LIMIT) set({ fields: [...draft.fields, selector] });
                    } else {
                      set({ fields: draft.fields.filter((f) => f !== selector) });
                    }
                  }}
                />
                {label}
              </label>
            );
          })}
        </div>
        <div className="custom-field">
          <label htmlFor={`${id}-custom`}>Another field</label>
          <input
            id={`${id}-custom`}
            ref={customInput}
            type="text"
            placeholder="OBX[1]-3"
            value={draft.custom}
            disabled={busy || draft.fields.length >= INDEX_FIELD_LIMIT}
            onChange={(e) => set({ custom: e.target.value })}
          />

          <button
            type="button"
            disabled={busy || !custom || draft.fields.includes(custom) || draft.fields.length >= INDEX_FIELD_LIMIT}
            onClick={() => {
              if (custom && !draft.fields.includes(custom) && draft.fields.length < INDEX_FIELD_LIMIT) {
                set({ fields: [...draft.fields, custom], custom: "" });
              }
            }}
          >
            Add field
          </button>
        </div>
        <div className="selected-fields-list" ref={fieldList}>
          {draft.fields.map((field, at) => (
            <span key={field} className="field-tag">
              <code>{field}</code>
              {/* Removes the selection from this unsaved definition only. */}
              <IconButton
                icon="close"
                label={`Remove indexed field ${field}`}
                disabled={busy}
                onClick={() => {
                  refocus.current = at;
                  set({ fields: draft.fields.filter((f) => f !== field) });
                }}
              />
            </span>
          ))}
        </div>
      </fieldset>

      <fieldset className="retention-selection">
        <legend>Search mode</legend>
        <label className="radio-label">
          <input
            type="radio"
            name={`${id}-retention`}
            value="states"
            checked={draft.retention === "states"}
            disabled={busy}
            onChange={() => set({ retention: "states" })}
          />
          <span>
            <strong>Presence only</strong>
            <span className="option-note">Stores field states, not values.</span>
          </span>
        </label>
        <label className="radio-label">
          <input
            type="radio"
            name={`${id}-retention`}
            value="digests"
            checked={draft.retention === "digests"}
            disabled={busy}
            onChange={() => set({ retention: "digests" })}
          />
          <span>
            <strong>Exact match</strong>
            <span className="option-note">Stores hashes for exact matching. Treat them as sensitive.</span>
          </span>
        </label>
        <label className="radio-label">
          <input
            type="radio"
            name={`${id}-retention`}
            value="values"
            checked={draft.retention === "values"}
            disabled={busy}
            onChange={() => set({ retention: "values" })}
          />
          <span>
            <strong>Full text</strong>
            <span className="option-note">Stores searchable values. May contain patient data.</span>
          </span>
        </label>
      </fieldset>

      <fieldset className="expiry-selection">
        <legend>Keep until</legend>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={draft.indefinite}
            disabled={busy}
            onChange={(e) => set({ indefinite: e.target.checked })}
          />
          No expiry
        </label>
        {!draft.indefinite ? (
          <div className="expiry-picker">
            <label htmlFor={`${id}-until`}>Keep until (local time)</label>
            <input
              id={`${id}-until`}
              type="datetime-local"
              step={1}
              aria-describedby={passed ? `${id}-until-utc ${id}-until-passed` : `${id}-until-utc`}
              value={draft.retainUntil}
              disabled={busy}
              onChange={(e) => set({ retainUntil: e.target.value })}
            />
            <span id={`${id}-until-utc`} className="hint">
              {typeof until === "string"
                ? `Stored as ${until} (UTC)`
                : "Enter a complete date and time."}
            </span>
            {passed ? (
              <span id={`${id}-until-passed`} className="hint">
                {"This date and time has passed. Enter a later one or choose No expiry."}
              </span>
            ) : null}
          </div>
        ) : null}
      </fieldset>

      <div className="output-selection">
        <label htmlFor={`${id}-output`}>File name</label>
        <input
          id={`${id}-output`}
          type="text"
          value={draft.output}
          disabled={busy}
          onChange={(e) => set({ output: e.target.value })}
        />
        {rebuilding ? (
          <>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={draft.replace}
                disabled={busy}
                aria-describedby={`${id}-replace-target`}
                onChange={(e) => set({ replace: e.target.checked })}
              />
              Replace selected index
            </label>
            <span id={`${id}-replace-target`} className="hint">
              {output === replaceTarget
                ? `Replaces ${replaceTarget} of case ${caseName}.`
                : `Only ${replaceTarget} of case ${caseName} can be replaced; another file name is written as a new index.`}
            </span>
          </>
        ) : null}
      </div>

      <div className="dialog-footer">
        {/* Closes the sheet; the definition above is kept and nothing that is
            running is cancelled. */}
        <button type="button" disabled={busy} onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className="primary" disabled={busy || draft.fields.length === 0 || !draft.output.trim() || !deadlineReady}>
          {replacing ? "Rebuild index" : "Build index"}
        </button>
      </div>
    </form>
  );
}

/** How an occurrence kind reads in the filter editor. The value sent is the
 * kind itself; only its caption is capitalized. */
export const KIND_CAPTIONS: Record<OccurrenceKind, string> = {
  message: "Message",
  ack: "ACK",
  unparsed: "Unparsed",
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
