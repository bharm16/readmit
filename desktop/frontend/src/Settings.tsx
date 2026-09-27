// Settings → General and Security. General shows the saved theme, text size and
// local reviewer name; Edit previews a change and Save keeps it. Security lists
// the connections this computer has configured, as they stand now, from saved
// configuration and the window's own state: reading it reaches nothing. Privacy
// values and saved searches are edited in one sheet; encryption controls are
// their own page.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  clearViews,
  disconnectHub,
  disconnectOperatorHub,
  listConnections,
  listSearchSettings,
  readPreferences,
  RequestScope,
  savePreferences,
  saveSearchSettings,
  type CaseSearchSettings,
  type ConnectionKind,
  type ConnectionOwnerKind,
  type ConnectionRow,
  type ConnectionState,
  type IndexRetention,
  type OperationDisclosure,
  type Preferences,
  type Theme,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, humanize, type SubmitFailure } from "./layout";
import { listDate } from "./Projects";
import { instantOf } from "./Messages";
import { CHECK_OUTCOMES, TRANSPORTS } from "./Environments";

// ---------- Preferences ----------

const DEFAULT_PREFERENCES: Preferences = { theme: "system", text_scale: 100 };

export function themeLabel(theme: Theme): string {
  return theme === "system" ? "System" : theme === "light" ? "Light" : theme === "dark" ? "Dark" : humanize(theme);
}

/** The saved preferences, and what the window shows now: the saved values, or
 * the values an open Edit sheet is previewing. */
export function usePreferences() {
  const [saved, setSaved] = useState<Preferences>(DEFAULT_PREFERENCES);
  const [preview, setPreview] = useState<Preferences | null>(null);
  useEffect(() => {
    let live = true;
    void readPreferences().then((answer) => {
      if (live && answer.state === "completed") setSaved(answer.preferences);
    });
    return () => {
      live = false;
    };
  }, []);
  const shown = preview ?? saved;
  useEffect(() => {
    if (shown.theme === "system") document.documentElement.removeAttribute("data-theme");
    else document.documentElement.setAttribute("data-theme", shown.theme);
  }, [shown.theme]);
  useEffect(() => {
    document.documentElement.style.setProperty("--text-scale", String(shown.text_scale / 100));
  }, [shown.text_scale]);
  const save = useCallback(async (next: Preferences): Promise<SubmitFailure | null> => {
    const answer = await savePreferences(next);
    if (answer.state !== "completed") return { reason: answer.reason ?? "Not saved." };
    setSaved(answer.preferences);
    setPreview(null);
    return null;
  }, []);
  return { saved, shown, setPreview, save };
}

export type PreferencesState = ReturnType<typeof usePreferences>;

/** General: saved values, one Edit sheet, and About and Check update. */
export function GeneralView({
  preferences,
  themes,
  scales,
  version,
  canUpdate,
  onCheckUpdate,
}: {
  preferences: PreferencesState;
  themes: Theme[];
  scales: number[];
  version: string;
  canUpdate: boolean;
  onCheckUpdate: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [about, setAbout] = useState(false);
  const { saved } = preferences;
  return (
    <>
      <div className="section-header">
        <h2>General</h2>
        <span className="row-actions">
          <button type="button" onClick={() => setEditing(true)}>
            Edit
          </button>
          <Menu
            label="More general settings"
            items={[
              { label: "About", onSelect: () => setAbout(true) },
              { label: "Check update", onSelect: onCheckUpdate, disabled: !canUpdate },
            ]}
          />
        </span>
      </div>
      <ValueRows
        label="General"
        rows={[
          { label: "Theme", value: themeLabel(saved.theme) },
          { label: "Text size", value: `${saved.text_scale}%` },
          ...(saved.reviewer ? [{ label: "Reviewer", value: saved.reviewer }] : []),
        ]}
      />
      <GeneralSheet open={editing} preferences={preferences} themes={themes} scales={scales} onClose={() => setEditing(false)} />
      <Modal
        open={about}
        title="About Readmit"
        size="small"
        onClose={() => setAbout(false)}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={() => setAbout(false)}>
              Close
            </button>
          </div>
        }
      >
        <ValueRows
          rows={[
            { label: "Application", value: "Readmit" },
            { label: "Version", value: version || "—" },
          ]}
        />
      </Modal>
    </>
  );
}

/** Theme, text size and reviewer. A choice previews at once; Cancel puts the
 * saved values back and Save keeps them. */
function GeneralSheet({
  open,
  preferences,
  themes,
  scales,
  onClose,
}: {
  open: boolean;
  preferences: PreferencesState;
  themes: Theme[];
  scales: number[];
  onClose: () => void;
}) {
  const { saved, setPreview, save } = preferences;
  const [draft, setDraft] = useState<Preferences>(saved);
  useEffect(() => {
    if (open) setDraft(saved);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  // A saved size the sheet does not offer stays selectable.
  const offered = scales.includes(saved.text_scale) ? scales : [...scales, saved.text_scale].sort((a, b) => a - b);
  const change = (next: Preferences) => {
    setDraft(next);
    setPreview(next);
  };
  const close = () => {
    setPreview(null);
    onClose();
  };
  const reviewer = (draft.reviewer ?? "").trim();
  const dirty = draft.theme !== saved.theme || draft.text_scale !== saved.text_scale || reviewer !== (saved.reviewer ?? "");
  return (
    <FormDialog
      open={open}
      title="General"
      size="small"
      submitLabel="Save"
      dirty={dirty}
      onClose={close}
      onDiscard={() => setPreview(null)}
      onSubmit={async () => {
        const failure = await save({ theme: draft.theme, text_scale: draft.text_scale, ...(reviewer ? { reviewer } : {}) });
        if (!failure) onClose();
        return failure;
      }}
    >
      <label htmlFor="theme">Theme</label>
      <select id="theme" value={draft.theme} onChange={(event) => change({ ...draft, theme: event.target.value as Theme })}>
        {themes.map((choice) => (
          <option key={choice} value={choice}>
            {themeLabel(choice)}
          </option>
        ))}
      </select>
      <label htmlFor="text-size">Text size</label>
      <select id="text-size" value={draft.text_scale} onChange={(event) => change({ ...draft, text_scale: Number(event.target.value) })}>
        {offered.map((percent) => (
          <option key={percent} value={percent}>
            {percent}%
          </option>
        ))}
      </select>
      <label htmlFor="reviewer">Reviewer</label>
      <input id="reviewer" type="text" maxLength={200} value={draft.reviewer ?? ""} onChange={(event) => setDraft({ ...draft, reviewer: event.target.value })} />
    </FormDialog>
  );
}

// ---------- Security ----------

const KINDS: Record<ConnectionKind, string> = {
  environment: "Environment",
  source: "Source",
  team: "Team",
  runner: "Runner",
  portal: "Customer portal",
  run: "Run",
  program: "Program",
};

export function connectionStatus(row: ConnectionRow): string {
  const labels: Record<ConnectionState, string> = {
    active: "Active",
    connected: "Connected",
    disconnected: "Disconnected",
    checked: "Checked",
    "not-checked": "Not checked",
    unavailable: "Unavailable",
  };
  if (row.state === "checked" && row.checked_at) return `Checked ${listDate(row.checked_at)}`;
  return labels[row.state];
}

/** Where Add connection and a row's Edit go: the owner of that kind's setup. */
export type ConnectionRoute =
  | { kind: "environment"; id?: string }
  | { kind: "observation"; id?: string }
  | { kind: "team" }
  | { kind: "runner" }
  | { kind: "license" };

const EDIT_ROUTES: Record<ConnectionOwnerKind, ((id?: string) => ConnectionRoute) | null> = {
  environment: (id) => (id ? { kind: "environment", id } : { kind: "environment" }),
  observation: (id) => (id ? { kind: "observation", id } : { kind: "observation" }),
  team: () => ({ kind: "team" }),
  license: () => ({ kind: "license" }),
  runner: () => ({ kind: "runner" }),
  source: null,
  run: null,
  portal: null,
  program: null,
};

function editRoute(row: ConnectionRow): ConnectionRoute | null {
  return EDIT_ROUTES[row.owner.kind]?.(row.owner.object_id) ?? null;
}

/** Security: the configured connections, privacy values and a way to the
 * encryption controls. */
export function SecurityView({
  root,
  busy,
  operations,
  onOpen,
  onEncryption,
}: {
  root: string | null;
  busy: boolean;
  operations: OperationDisclosure[];
  onOpen: (route: ConnectionRoute) => void;
  onEncryption: () => void;
}) {
  const scope = useRef(new RequestScope());
  const [rows, setRows] = useState<ConnectionRow[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [privacy, setPrivacy] = useState(false);
  const [searches, setSearches] = useState<CaseSearchSettings[] | null>(null);
  const [projectReason, setProjectReason] = useState<string | null>(null);
  const searchReads = useRef(0);

  const refresh = useCallback(async () => {
    const answer = await listConnections(scope.current.enter(root ?? ""));
    if (!scope.current.current(answer)) return;
    if (answer.state === "completed" || answer.state === "empty") {
      setRows(answer.rows);
      setProjectReason(answer.project_reason ?? null);
      setFailure(null);
    } else {
      setFailure(answer.reason ?? "The connections could not be read.");
    }
  }, [root]);

  const refreshSearches = useCallback(async () => {
    if (!root) {
      setSearches(null);
      return;
    }
    const read = ++searchReads.current;
    const answer = await listSearchSettings(root);
    if (read !== searchReads.current) return;
    // A busy answer keeps what was read before; the next settle reads again.
    if (answer.state === "completed" || answer.state === "empty") setSearches(answer.cases);
    else if (answer.state !== "busy") setSearches(null);
  }, [root]);

  // An operation starting or ending changes what is reaching a destination.
  useEffect(() => {
    void refresh();
  }, [refresh, busy]);
  useEffect(() => {
    if (!busy) void refreshSearches();
  }, [refreshSearches, busy]);

  const row = rows?.find((entry) => entry.ref === selected) ?? null;
  const columns: Column<ConnectionRow>[] = [
    {
      key: "name",
      header: "Name",
      priority: 1,
      minWidth: 12.5,
      render: (entry) => (
        <span className="case-name">
          <span>{entry.name}</span>
          {entry.state === "unavailable" && entry.reason ? <span className="row-reason">{entry.reason}</span> : null}
        </span>
      ),
    },
    { key: "destination", header: "Destination", priority: 2, minWidth: 12.5, render: (entry) => entry.destination || "—" },
    { key: "status", header: "Status", priority: 1, minWidth: 9, render: connectionStatus },
  ];

  let list: ReactNode;
  if (failure) {
    list = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{failure}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (rows && rows.length === 0) {
    list = <EmptyState title="No connections" action={<AddConnection busy={busy} onOpen={onOpen} />} />;
  } else {
    list = (
      <DataTable
        label="Connections"
        className="values-table"
        rows={rows ?? []}
        rowId={(entry) => entry.ref}
        rowLabel={(entry) => entry.name}
        columns={columns}
        selected={selected}
        onSelect={setSelected}
        onOpen={setSelected}
        loading={rows === null}
      />
    );
  }

  return (
    <>
      <section className="value-group" aria-labelledby="security-connections">
        <header className="value-group-header">
          <h2 id="security-connections">Connections</h2>
          <span className="row-actions">
            <IconButton icon="refresh" label="Refresh status" onClick={() => void refresh()} />
            {rows && rows.length > 0 ? <AddConnection busy={busy} onOpen={onOpen} /> : null}
          </span>
        </header>
        {projectReason ? (
          <p role="alert" className="object-problem">
            {projectReason}
          </p>
        ) : null}
        {list}
      </section>
      <section className="value-group" aria-labelledby="security-privacy">
        <header className="value-group-header">
          <h2 id="security-privacy">Privacy</h2>
          <button type="button" disabled={busy || !root} onClick={() => setPrivacy(true)}>
            Edit
          </button>
        </header>
        <ValueRows
          label="Privacy"
          rows={[
            { label: "Message values", value: "Hidden by default" },
            { label: "Saved views", value: "This project" },
          ]}
        />
      </section>
      {root ? (
        <ul className="launcher" aria-label="Encryption">
          <li>
            <button type="button" className="launcher-row" onClick={onEncryption}>
              <span>Encryption</span>
              <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
                <path d="M6 3l5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
          </li>
        </ul>
      ) : null}
      <ConnectionDetail
        row={row}
        operations={operations}
        busy={busy}
        onClose={() => setSelected(null)}
        onEdit={(entry) => {
          const route = editRoute(entry);
          if (route) {
            setSelected(null);
            onOpen(route);
          }
        }}
        onDisconnected={() => void refresh()}
      />
      {root ? (
        <PrivacySheet
          open={privacy}
          root={root}
          cases={searches ?? []}
          onClose={() => setPrivacy(false)}
          onSaved={() => void refreshSearches()}
        />
      ) : null}
    </>
  );
}

function AddConnection({ busy, onOpen }: { busy: boolean; onOpen: (route: ConnectionRoute) => void }) {
  return (
    <Menu
      label="Add connection"
      trigger={<span>Add connection</span>}
      items={[
        { label: "Environment", onSelect: () => onOpen({ kind: "environment" }), disabled: busy },
        { label: "Source", onSelect: () => onOpen({ kind: "observation" }), disabled: busy },
        { label: "Team", onSelect: () => onOpen({ kind: "team" }) },
        { label: "Runner", onSelect: () => onOpen({ kind: "runner" }) },
      ]}
    />
  );
}

/** One connection, read-only: what it is, where it goes, what reached it last
 * and what it may carry. */
function ConnectionDetail({
  row,
  operations,
  busy,
  onClose,
  onEdit,
  onDisconnected,
}: {
  row: ConnectionRow | null;
  operations: OperationDisclosure[];
  busy: boolean;
  onClose: () => void;
  onEdit: (row: ConnectionRow) => void;
  onDisconnected: () => void;
}) {
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => setProblem(null), [row?.ref]);
  const disclosure = operations.find((operation) => operation.id === row?.disclosure);
  const detail = row?.detail;
  const disconnect = async () => {
    if (!row) return;
    const answer = row.ref === "hub:operator" ? await disconnectOperatorHub() : await disconnectHub();
    if (answer.state === "completed") {
      onDisconnected();
      onClose();
    } else {
      setProblem(answer.reason ?? "Not disconnected.");
    }
  };
  return (
    <Modal
      open={row !== null}
      title={row?.name ?? "Connection"}
      onClose={onClose}
      footer={
        row ? (
          <>
            {problem ? (
              <p className="dialog-status" role="alert">
                {problem}
              </p>
            ) : null}
            <div className="dialog-footer">
              {row.actions.includes("disconnect") ? (
                <button type="button" disabled={busy} onClick={() => void disconnect()}>
                  Disconnect
                </button>
              ) : null}
              {row.actions.includes("edit") && editRoute(row) ? (
                <button type="button" className="primary" disabled={busy} onClick={() => onEdit(row)}>
                  Edit
                </button>
              ) : null}
            </div>
          </>
        ) : null
      }
    >
      {row ? (
        <ValueRows
          rows={[
            { label: "Type", value: KINDS[row.kind] },
            { label: "Destination", value: row.destination || "—" },
            { label: "Status", value: connectionStatus(row) },
            ...(row.reason ? [{ label: "Reason", value: row.reason }] : []),
            ...(row.checked_at
              ? [{ label: "Last result", value: `${detail?.outcome && CHECK_OUTCOMES[detail.outcome] ? CHECK_OUTCOMES[detail.outcome] + " · " : ""}${new Date(row.checked_at).toLocaleString()}` }]
              : []),
            ...(row.last_seen ? [{ label: "Last seen", value: new Date(row.last_seen).toLocaleString() }] : []),
            ...(detail?.operation ? [{ label: "Activity", value: detail.operation }] : []),
            ...(disclosure ? [{ label: "Data", value: disclosure.data }] : []),
            ...(disclosure ? [{ label: "Authorization", value: disclosure.authorization }] : []),
            ...(detail?.transport && TRANSPORTS.some((entry) => entry.value === detail.transport)
              ? [{ label: "Transport", value: TRANSPORTS.find((entry) => entry.value === detail.transport)!.label }]
              : []),
            ...(detail?.config_path ? [{ label: "Configuration", value: detail.config_path }] : []),
          ]}
        />
      ) : null}
    </Modal>
  );
}

/** An instant as a local datetime-local value, the form the field reads back. */
function localInput(instant: string): string {
  const date = new Date(instant);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

const STORED_AS: { value: IndexRetention; label: string }[] = [
  { value: "states", label: "States" },
  { value: "digests", label: "Hashes" },
  { value: "values", label: "Values" },
];

/** Saved searches: which fields a case's search keeps, in what form and until
 * when, and clearing this project's saved views. */
function PrivacySheet({
  open,
  root,
  cases,
  onClose,
  onSaved,
}: {
  open: boolean;
  root: string;
  cases: CaseSearchSettings[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [caseName, setCaseName] = useState("");
  const [fields, setFields] = useState<string[]>([""]);
  const [retention, setRetention] = useState<IndexRetention>("states");
  const [indefinite, setIndefinite] = useState(false);
  const [until, setUntil] = useState("");
  const [clearing, setClearing] = useState(false);
  const [cleared, setCleared] = useState<string | null>(null);
  const chosen = cases.find((entry) => entry.case === caseName) ?? null;

  const prefill = (entry: CaseSearchSettings | null) => {
    const settings = entry?.settings ?? null;
    setFields(settings && settings.fields.length > 0 ? [...settings.fields] : [""]);
    setRetention(settings?.retention ?? "states");
    setIndefinite(settings !== null && settings.retain_until === null);
    // An expired end is never carried forward: a new end is chosen deliberately.
    setUntil(settings?.retain_until && !settings.expired ? localInput(settings.retain_until) : "");
  };
  useEffect(() => {
    if (!open) return;
    const first = cases[0] ?? null;
    setCaseName(first?.case ?? "");
    prefill(first);
    setCleared(null);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const picked = fields.map((field) => field.trim()).filter((field) => field !== "");
  return (
    <>
      <FormDialog
        open={open && !clearing}
        title="Privacy"
        submitLabel="Save"
        submitDisabled={chosen === null || picked.length === 0 || (!indefinite && until === "")}
        onClose={onClose}
        status={cleared ? <p role="status">{cleared}</p> : null}
        onSubmit={async (): Promise<SubmitFailure | null> => {
          if (!chosen) return { reason: "Choose a case." };
          let end = "indefinite";
          if (!indefinite) {
            const instant = instantOf(until, "local");
            if (!instant) return { reason: "Enter a complete date and time.", field: "saved-search-until" };
            end = instant;
          }
          const answer = await saveSearchSettings({
            workspace: root,
            case: chosen.case,
            identity: chosen.identity ?? "",
            fields: picked,
            retention,
            retain_until: end,
          });
          if (answer.state !== "completed") return { reason: answer.reason ?? "Not saved." };
          onSaved();
          onClose();
          return null;
        }}
      >
        {cases.length === 0 ? (
          <p>No cases</p>
        ) : (
          <>
            <label htmlFor="saved-search-case">Case</label>
            <select
              id="saved-search-case"
              value={caseName}
              onChange={(event) => {
                setCaseName(event.target.value);
                prefill(cases.find((entry) => entry.case === event.target.value) ?? null);
              }}
            >
              {cases.map((entry) => (
                <option key={entry.case} value={entry.case}>
                  {entry.case}
                </option>
              ))}
            </select>
            {chosen?.reason ? <p role="alert">{chosen.reason}</p> : null}
            {chosen && chosen.settings === null && !chosen.reason ? <ValueRows rows={[{ label: "Saved search", value: "None" }]} /> : null}
            <fieldset>
              <legend>Search fields</legend>
              {fields.map((field, index) => (
                <div key={index} className="filter-rule-row">
                  <input
                    type="text"
                    aria-label={`Field ${index + 1}`}
                    spellCheck={false}
                    value={field}
                    onChange={(event) => setFields((held) => held.map((value, at) => (at === index ? event.target.value : value)))}
                  />
                  <IconButton
                    icon="close"
                    label={`Remove field ${index + 1}`}
                    onClick={() => setFields((held) => (held.length > 1 ? held.filter((_, at) => at !== index) : [""]))}
                  />
                </div>
              ))}
              <button type="button" className="quiet" onClick={() => setFields((held) => [...held, ""])}>
                Add field
              </button>
            </fieldset>
            <label htmlFor="saved-search-stored">Stored as</label>
            <select id="saved-search-stored" value={retention} onChange={(event) => setRetention(event.target.value as IndexRetention)}>
              {STORED_AS.map((mode) => (
                <option key={mode.value} value={mode.value}>
                  {mode.label}
                </option>
              ))}
            </select>
            {retention !== "states" ? <p className="consequence">May contain patient data.</p> : null}
            <fieldset>
              <legend>Keep</legend>
              <label className="check">
                <input type="radio" name="saved-search-keep" checked={!indefinite} onChange={() => setIndefinite(false)} />
                Until
              </label>
              {!indefinite ? (
                <input id="saved-search-until" aria-label="Keep until" type="datetime-local" value={until} onChange={(event) => setUntil(event.target.value)} />
              ) : null}
              <label className="check">
                <input type="radio" name="saved-search-keep" checked={indefinite} onChange={() => setIndefinite(true)} />
                Indefinitely
              </label>
            </fieldset>
          </>
        )}
        <button type="button" className="quiet" onClick={() => setClearing(true)}>
          Clear saved searches
        </button>
      </FormDialog>
      <Modal
        open={open && clearing}
        title="Clear saved searches?"
        size="small"
        onClose={() => setClearing(false)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setClearing(false)}>
              Cancel
            </button>
            <button
              type="button"
              className="danger solid"
              onClick={async () => {
                const answer = await clearViews(root);
                setClearing(false);
                setCleared(answer.state === "failed" || answer.state === "busy" ? (answer.reason ?? "Not cleared.") : "Saved searches cleared.");
              }}
            >
              Clear
            </button>
          </div>
        }
      >
        <p>Removes this project's saved searches. Case evidence and exported files are unchanged.</p>
      </Modal>
    </>
  );
}
