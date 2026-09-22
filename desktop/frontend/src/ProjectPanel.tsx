// The project overview: the `readmit project show` of this window, rendered
// from what the facade re-read from disk. Creating a project, editing its
// settings, registering a case and updating what a case records are all typed
// calls into the shared Go operations the command line runs; nothing here
// decides what a project can hold. The panel shows positions and states —
// titles, tags, statuses, identities — never evidence content.
import { useState } from "react";
import type {
  Artifact,
  CaseChange,
  CaseRegistration,
  CaseStatus,
  ProjectOverview,
  ProjectOverviewResult,
  SettingsChange,
} from "./bindings";
import type { Indicators } from "./shell";
import { Badge, Report } from "./shell";

/** The entries of the open workspace a new case could come from: the listing's
 * case bundles the project does not register already. */
function registrable(overview: ProjectOverview | null, entries: Artifact[]): Artifact[] {
  const registered = new Set((overview?.cases ?? []).map((entry) => entry.name));
  return entries.filter((artifact) => artifact.kind === "case" && !registered.has(artifact.name));
}

/** The breadcrumb trail of where the investigation is. Each earlier crumb is a
 * place the window can go back to, so the way in is also the way out. */
export function Breadcrumbs({
  project,
  selectedCase,
  importing,
  capturing,
  observing,
  maintaining,
  onWorkspace,
  onProject,
}: {
  project: string | null;
  selectedCase: string | null;
  importing?: boolean;
  capturing?: boolean;
  observing?: boolean;
  maintaining?: boolean;
  onWorkspace: () => void;
  onProject: () => void;
}) {
  if (!project) {
    return null;
  }
  return (
    <nav className="breadcrumbs" aria-label="Where you are">
      <button type="button" onClick={onWorkspace}>
        Workspace
      </button>
      <span aria-hidden="true">›</span>
      {capturing ? (
        <>
          <button type="button" onClick={onProject}>
            {project}
          </button>
          <span aria-hidden="true">›</span>
          <span aria-current="page">Capture and collect</span>
        </>
      ) : maintaining ? (
        <>
          <button type="button" onClick={onProject}>
            {project}
          </button>
          <span aria-hidden="true">›</span>
          <span aria-current="page">Maintain workspace</span>
        </>
      ) : importing ? (
        <>
          <button type="button" onClick={onProject}>
            {project}
          </button>
          <span aria-hidden="true">›</span>
          <span aria-current="page">Import evidence</span>
        </>
      ) : observing ? (
        <>
          <button type="button" onClick={onProject}>
            {project}
          </button>
          <span aria-hidden="true">›</span>
          <span aria-current="page">Observation setup</span>
        </>
      ) : selectedCase ? (
        <>
          <button type="button" onClick={onProject}>
            {project}
          </button>
          <span aria-hidden="true">›</span>
          <span aria-current="page">{selectedCase}</span>
        </>
      ) : (
        <span aria-current="page">{project}</span>
      )}
    </nav>
  );
}

/** One registered case: what the project records beside what verification just
 * found, with the actions the investigation continues from. */
function CaseRow({
  caseEntry,
  versions,
  busy,
  indicators,
  onOpen,
  onUpdate,
}: {
  caseEntry: ProjectOverview["cases"][number];
  versions: string[];
  busy: boolean;
  indicators: Indicators;
  onOpen: () => void;
  onUpdate: (change: CaseChange) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<{
    title: string;
    owner: string;
    status: CaseStatus;
    interface_version: string;
    tags: string;
    incidents: string;
  }>({
    title: caseEntry.title,
    owner: caseEntry.owner ?? "",
    status: caseEntry.status,
    interface_version: caseEntry.interface_version,
    tags: caseEntry.tags.join(", "),
    incidents: caseEntry.incidents.join(", "),
  });
  const list = (value: string) =>
    value
      .split(",")
      .map((entry) => entry.trim())
      .filter((entry) => entry !== "");
  return (
    <li className="registered-case">
      <span className="name">{caseEntry.title}</span>
      <Badge indicator={indicators.get(caseEntry.status)} fallback={caseEntry.status} />
      <span className="badge">{caseEntry.interface_version}</span>
      <span className={`evidence evidence-${caseEntry.evidence}`}>{caseEntry.evidence}</span>
      <button type="button" disabled={busy} onClick={onOpen}>
        Open this case
      </button>
      <button type="button" disabled={busy} aria-expanded={editing} onClick={() => setEditing(!editing)}>
        {editing ? "Close the editor" : "Edit details"}
      </button>
      {editing ? (
        <form
          className="case-editor"
          aria-label={`Edit ${caseEntry.title}`}
          onSubmit={(event) => {
            event.preventDefault();
            // Only what changed is sent, so an edit changes exactly the
            // members the person named and nothing beside them.
            const change: CaseChange = {};
            if (draft.title !== caseEntry.title) {
              change.title = draft.title;
            }
            if (draft.owner !== (caseEntry.owner ?? "")) {
              change.owner = draft.owner;
            }
            if (draft.status !== caseEntry.status) {
              change.status = draft.status;
            }
            if (draft.interface_version !== caseEntry.interface_version) {
              change.interface_version = draft.interface_version;
            }
            if (list(draft.tags).join(",") !== caseEntry.tags.join(",")) {
              change.tags = list(draft.tags);
            }
            if (list(draft.incidents).join(",") !== caseEntry.incidents.join(",")) {
              change.incidents = list(draft.incidents);
            }
            onUpdate(change);
            setEditing(false);
          }}
        >
          <label htmlFor={`case-title-${caseEntry.name}`}>Title</label>
          <input
            id={`case-title-${caseEntry.name}`}
            type="text"
            value={draft.title}
            onChange={(event) => setDraft({ ...draft, title: event.target.value })}
          />
          <label htmlFor={`case-owner-${caseEntry.name}`}>Owner</label>
          <input
            id={`case-owner-${caseEntry.name}`}
            type="text"
            value={draft.owner}
            onChange={(event) => setDraft({ ...draft, owner: event.target.value })}
          />
          <label htmlFor={`case-status-${caseEntry.name}`}>Status</label>
          <select
            id={`case-status-${caseEntry.name}`}
            value={draft.status}
            onChange={(event) => setDraft({ ...draft, status: event.target.value as CaseStatus })}
          >
            {(["open", "investigating", "resolved", "closed"] as CaseStatus[]).map((status) => (
              <option key={status} value={status}>
                {status}
              </option>
            ))}
          </select>
          <label htmlFor={`case-version-${caseEntry.name}`}>Interface version</label>
          <select
            id={`case-version-${caseEntry.name}`}
            value={draft.interface_version}
            onChange={(event) => setDraft({ ...draft, interface_version: event.target.value })}
          >
            {versions.map((version) => (
              <option key={version} value={version}>
                {version}
              </option>
            ))}
          </select>
          <label htmlFor={`case-tags-${caseEntry.name}`}>Tags, comma-separated</label>
          <input
            id={`case-tags-${caseEntry.name}`}
            type="text"
            value={draft.tags}
            onChange={(event) => setDraft({ ...draft, tags: event.target.value })}
          />
          <label htmlFor={`case-incidents-${caseEntry.name}`}>Linked incidents, comma-separated</label>
          <input
            id={`case-incidents-${caseEntry.name}`}
            type="text"
            value={draft.incidents}
            onChange={(event) => setDraft({ ...draft, incidents: event.target.value })}
          />
          <button type="submit" disabled={busy}>
            Store these details
          </button>
        </form>
      ) : null}
    </li>
  );
}

/** The create form. Submitting asks the facade for the parent folder with the
 * host's own dialog; the fields here are only what the project document holds. */
function CreateForm({
  busy,
  onCreate,
}: {
  busy: boolean;
  onCreate: (name: string, title: string, owner: string, versions: string[]) => void;
}) {
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [owner, setOwner] = useState("");
  const [versions, setVersions] = useState("");
  return (
    <form
      className="project-create"
      aria-label="Create a project"
      onSubmit={(event) => {
        event.preventDefault();
        onCreate(
          name.trim(),
          title.trim(),
          owner.trim(),
          versions
            .split(",")
            .map((version) => version.trim())
            .filter((version) => version !== ""),
        );
      }}
    >
      <h4>Create a project</h4>
      <label htmlFor="project-name">Folder name for the new project</label>
      <input
        id="project-name"
        type="text"
        value={name}
        onChange={(event) => setName(event.target.value)}
      />
      <label htmlFor="project-title">Title</label>
      <input
        id="project-title"
        type="text"
        value={title}
        onChange={(event) => setTitle(event.target.value)}
      />
      <label htmlFor="project-owner">Default owner</label>
      <input
        id="project-owner"
        type="text"
        value={owner}
        onChange={(event) => setOwner(event.target.value)}
      />
      <label htmlFor="project-versions">Interface versions, comma-separated</label>
      <input
        id="project-versions"
        type="text"
        value={versions}
        onChange={(event) => setVersions(event.target.value)}
      />
      <button type="submit" disabled={busy}>
        Create the project…
      </button>
      <p className="hint">The folder that holds it is chosen in your own folder dialog.</p>
    </form>
  );
}

/** The settings editor. A field left as it is sends nothing, so a settings
 * edit changes only what it names. */
function SettingsForm({
  overview,
  busy,
  onSave,
}: {
  overview: ProjectOverview;
  busy: boolean;
  onSave: (change: SettingsChange) => void;
}) {
  const [title, setTitle] = useState(overview.title);
  const [owner, setOwner] = useState(overview.default_owner ?? "");
  const [version, setVersion] = useState(overview.default_version ?? "");
  const [newVersion, setNewVersion] = useState("");
  return (
    <form
      className="project-settings"
      aria-label="Project settings"
      onSubmit={(event) => {
        event.preventDefault();
        const declared = newVersion.trim();
        // A field left as it was sends nothing, so a settings edit changes
        // only what it names.
        const change: SettingsChange = {};
        if (title !== overview.title) {
          change.title = title;
        }
        if (owner !== (overview.default_owner ?? "")) {
          change.default_owner = owner;
        }
        if (version !== (overview.default_version ?? "")) {
          change.default_interface_version = version;
        }
        if (declared !== "") {
          change.declare_versions = [declared];
        }
        onSave(change);
        setNewVersion("");
      }}
    >
      <h4>Project settings</h4>
      <label htmlFor="settings-title">Title</label>
      <input
        id="settings-title"
        type="text"
        value={title}
        onChange={(event) => setTitle(event.target.value)}
      />
      <label htmlFor="settings-owner">Default owner</label>
      <input
        id="settings-owner"
        type="text"
        value={owner}
        onChange={(event) => setOwner(event.target.value)}
      />
      <label htmlFor="settings-version">Default interface version</label>
      <select
        id="settings-version"
        value={version}
        onChange={(event) => setVersion(event.target.value)}
      >
        <option value="">None</option>
        {overview.interface_versions.map((declared) => (
          <option key={declared} value={declared}>
            {declared}
          </option>
        ))}
      </select>
      <label htmlFor="settings-declare">Declare a further interface version</label>
      <input
        id="settings-declare"
        type="text"
        value={newVersion}
        onChange={(event) => setNewVersion(event.target.value)}
      />
      <button type="submit" disabled={busy}>
        Store these settings
      </button>
      <p className="hint">
        A declared version is never removed: registered cases still name it. Declared here:{" "}
        {overview.interface_versions.join(", ")}
      </p>
    </form>
  );
}

/** The registration form: pick one case bundle the workspace lists and the
 * project does not register yet, and record what a person maintains about it.
 * The evidence facts are read from the bundle by the shared reader. */
function RegisterForm({
  candidates,
  versions,
  defaultVersion,
  defaultOwner,
  busy,
  onRegister,
}: {
  candidates: Artifact[];
  versions: string[];
  defaultVersion: string;
  defaultOwner: string;
  busy: boolean;
  onRegister: (name: string, registration: CaseRegistration) => void;
}) {
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [owner, setOwner] = useState("");
  const [status, setStatus] = useState<CaseStatus | "">("");
  const [version, setVersion] = useState("");
  const [tags, setTags] = useState("");
  const [incidents, setIncidents] = useState("");
  const list = (value: string) =>
    value
      .split(",")
      .map((entry) => entry.trim())
      .filter((entry) => entry !== "");
  return (
    <form
      className="project-register"
      aria-label="Register a case"
      onSubmit={(event) => {
        event.preventDefault();
        if (name === "") {
          return;
        }
        const registration: CaseRegistration = { title: title.trim() };
        if (owner.trim() !== "") {
          registration.owner = owner.trim();
        }
        if (status !== "") {
          registration.status = status;
        }
        if (version !== "") {
          registration.interface_version = version;
        }
        registration.tags = list(tags);
        registration.incidents = list(incidents);
        onRegister(name, registration);
        setTitle("");
        setTags("");
        setIncidents("");
      }}
    >
      <h4>Register a case</h4>
      <label htmlFor="register-case">Case bundle in this workspace</label>
      <select
        id="register-case"
        value={name}
        onChange={(event) => setName(event.target.value)}
        disabled={busy || candidates.length === 0}
      >
        <option value="">Choose a case bundle…</option>
        {candidates.map((artifact) => (
          <option key={artifact.name} value={artifact.name}>
            {artifact.name}
          </option>
        ))}
      </select>
      <label htmlFor="register-title">Title</label>
      <input
        id="register-title"
        type="text"
        value={title}
        onChange={(event) => setTitle(event.target.value)}
      />
      <label htmlFor="register-owner">Owner</label>
      <input
        id="register-owner"
        type="text"
        placeholder={defaultOwner}
        value={owner}
        onChange={(event) => setOwner(event.target.value)}
      />
      <label htmlFor="register-status">Status</label>
      <select
        id="register-status"
        value={status}
        onChange={(event) => setStatus(event.target.value as CaseStatus | "")}
      >
        <option value="">Open — the project default</option>
        {(["open", "investigating", "resolved", "closed"] as CaseStatus[]).map((choice) => (
          <option key={choice} value={choice}>
            {choice}
          </option>
        ))}
      </select>
      <label htmlFor="register-version">Interface version</label>
      <select
        id="register-version"
        value={version}
        onChange={(event) => setVersion(event.target.value)}
      >
        <option value="">{defaultVersion} — the project default</option>
        {versions.map((declared) => (
          <option key={declared} value={declared}>
            {declared}
          </option>
        ))}
      </select>
      <label htmlFor="register-tags">Tags, comma-separated</label>
      <input
        id="register-tags"
        type="text"
        value={tags}
        onChange={(event) => setTags(event.target.value)}
      />
      <label htmlFor="register-incidents">Linked incidents, comma-separated</label>
      <input
        id="register-incidents"
        type="text"
        value={incidents}
        onChange={(event) => setIncidents(event.target.value)}
      />
      <button type="submit" disabled={busy || name === ""}>
        Register this case
      </button>
      <p className="hint">
        The bundle is verified through the same reader the command line uses, and
        the identity it declares is what the project records.
      </p>
    </form>
  );
}

/** The whole project context: what the project holds, and the actions that
 * move the investigation forward from here. */
export function ProjectPanel({
  root,
  result,
  entries,
  busy,
  progress,
  indicators,
  selectedCase,
  onCreate,
  onUpdateSettings,
  onRegister,
  onUpdateCase,
  onOpenCase,
  onStartImport,
  onStartCapture,
  onStartObservation,
  onStartMaintenance,
}: {
  root: string | null;
  result: ProjectOverviewResult | null;
  entries: Artifact[];
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  selectedCase: string | null;
  onCreate: (name: string, title: string, owner: string, versions: string[]) => void;
  onUpdateSettings: (change: SettingsChange) => void;
  onRegister: (name: string, registration: CaseRegistration) => void;
  onUpdateCase: (name: string, change: CaseChange) => void;
  onOpenCase: (name: string) => void;
  onStartImport?: () => void;
  onStartCapture?: () => void;
  onStartObservation?: () => void;
  onStartMaintenance?: () => void;
}) {
  const [creating, setCreating] = useState(false);
  const [editingSettings, setEditingSettings] = useState(false);
  const overview = result?.overview ?? null;
  const candidates = registrable(overview, entries);

  if (!root) {
    return (
      <p className="hint">
        Open a workspace folder to begin. A folder that holds a project document
        opens as that project; any folder can still be inspected as it is, and
        the guided sample stays free.
      </p>
    );
  }
  if (!result && !busy) {
    return (
      <div className="project">
        <p className="hint">
          This folder is open as a workspace. Open its project, or create one:
          a project is where cases are registered, named and carried forward.
        </p>
        <button type="button" disabled={busy} aria-expanded={creating} onClick={() => setCreating(!creating)}>
          {creating ? "Close the project form" : "Create a project…"}
        </button>
        {onStartImport ? (
          <button type="button" disabled={busy} onClick={onStartImport} style={{ marginLeft: "0.5rem" }}>
            Import evidence…
          </button>
        ) : null}
        {onStartCapture ? (
          <button type="button" disabled={busy} onClick={onStartCapture} style={{ marginLeft: "0.5rem" }}>
            Capture or collect evidence…
          </button>
        ) : null}
        {onStartObservation ? (
          <button type="button" disabled={busy} onClick={onStartObservation} style={{ marginLeft: "0.5rem" }}>
            Set up observation…
          </button>
        ) : null}
        {creating ? <CreateForm busy={busy} onCreate={onCreate} /> : null}
      </div>
    );
  }
  return (
    <div className="project">
      <Report indicators={indicators} progress={progress} result={result} />
      {overview ? (
        <>
          <h3>{overview.title}</h3>
          <p className="reason">
            {overview.default_version ? `default interface ${overview.default_version} · ` : ""}
            {overview.cases.length} registered {overview.cases.length === 1 ? "case" : "cases"} ·{" "}
            {overview.revisions.length} {overview.revisions.length === 1 ? "revision" : "revisions"} ·{" "}
            {overview.notes.length} {overview.notes.length === 1 ? "note" : "notes"}
          </p>
          <button type="button" disabled={busy} aria-expanded={editingSettings} onClick={() => setEditingSettings(!editingSettings)}>
            {editingSettings ? "Close the settings" : "Edit settings…"}
          </button>
          {editingSettings ? (
            <SettingsForm overview={overview} busy={busy} onSave={onUpdateSettings} />
          ) : null}

          <h4>Registered cases</h4>
          {overview.cases.length === 0 ? (
            <p className="hint">
              Nothing is registered yet. Choose a case bundle from this workspace
              below — the investigation starts from real evidence.
            </p>
          ) : (
            <ul className="registered">
              {overview.cases.map((caseEntry) => (
                <CaseRow
                  key={caseEntry.name}
                  caseEntry={caseEntry}
                  versions={overview.interface_versions}
                  busy={busy}
                  indicators={indicators}
                  onOpen={() => onOpenCase(caseEntry.name)}
                  onUpdate={(change) => onUpdateCase(caseEntry.name, change)}
                />
              ))}
            </ul>
          )}
          <RegisterForm
            candidates={candidates}
            versions={overview.interface_versions}
            defaultVersion={overview.default_version ?? ""}
            defaultOwner={overview.default_owner ?? ""}
            busy={busy}
            onRegister={onRegister}
          />

          <div style={{ marginTop: "1rem" }}>
            {onStartCapture ? (
              <button type="button" disabled={busy} onClick={onStartCapture}>
                Capture or collect evidence…
              </button>
            ) : null}
            {onStartImport ? (
              <button
                type="button"
                disabled={busy}
                onClick={onStartImport}
                style={{ marginLeft: onStartCapture ? "0.5rem" : undefined }}
              >
                Import evidence into this project…
              </button>
            ) : null}
            {onStartObservation ? (
              <button
                type="button"
                disabled={busy}
                onClick={onStartObservation}
                style={{ marginLeft: "0.5rem" }}
              >
                Set up observation…
              </button>
            ) : null}
            {onStartMaintenance ? (
              <button
                type="button"
                disabled={busy}
                onClick={onStartMaintenance}
                style={{ marginLeft: "0.5rem" }}
              >
                Maintain this workspace…
              </button>
            ) : null}
          </div>

          <h4>Revisions</h4>
          {overview.revisions.length === 0 ? (
            <p className="hint">
              No revisions registered. A reproducer you build can be registered
              as a revision of its case from the reproducer panel beside the
              verified case, and it is navigable here once it is.
            </p>
          ) : (
            <ul className="revisions">
              {overview.revisions.map((revision) => (
                <li key={revision.name}>
                  <span className="name">{revision.name}</span>
                  <span className={`evidence evidence-${revision.evidence}`}>{revision.evidence}</span>
                  <span className="badge">
                    {revision.operation} of {revision.parent}
                  </span>
                  <button type="button" disabled={busy} onClick={() => onOpenCase(revision.name)}>
                    Open this revision
                  </button>
                </li>
              ))}
            </ul>
          )}

          <h4>Notes</h4>
          {overview.notes.length === 0 ? (
            <p className="hint">
              No notes yet. Notes you write below are stored in the project&apos;s
              own editable document, never inside evidence.
            </p>
          ) : (
            <ul className="notes">
              {overview.notes.map((note) => (
                <li key={note.name}>
                  <span className="name">{note.title}</span>
                  {note.subject ? <span className="badge">about {note.subject}</span> : null}
                </li>
              ))}
            </ul>
          )}
          {selectedCase ? (
            <p className="hint">
              {selectedCase} is open in the inspector. Continue from its
              occurrences: the grid, the sequence and the authoring panels all
              carry this case forward.
            </p>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
