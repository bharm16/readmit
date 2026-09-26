// The project overview: the `readmit project show` of this window, rendered
// from what the facade re-read from disk. Editing its settings, registering a
// case and updating what a case records are all typed calls into the shared Go
// operations the command line runs; nothing here decides what a project can
// hold. The panel shows positions and states — titles, tags, statuses,
// identities — never evidence content. Every edit happens in a sheet a person
// opens; the page itself only shows what the project holds.
import { useState } from "react";
import type {
  Artifact,
  CaseChange,
  CaseRegistration,
  CaseStatus,
  ProjectOverview,
  ProjectOverviewResult,
  RegisteredCase,
  RevisionsResult,
  SettingsChange,
} from "./bindings";
import type { Indicators } from "./shell";
import { Report } from "./shell";
import { EmptyState, FormDialog, humanize } from "./layout";
import { Field } from "./ui";

const STATUSES: CaseStatus[] = ["open", "investigating", "resolved", "closed"];

/** An operation's contract name as a person reads it: "readmit-reproducer/v1"
 * is a reproducer. */
function operationName(operation: string): string {
  return humanize(operation.replace(/^readmit-/, "").replace(/\/v\d+$/, ""));
}

/** Comma-separated text as the list it names. */
function list(value: string): string[] {
  return value
    .split(",")
    .map((entry) => entry.trim())
    .filter((entry) => entry !== "");
}

/** The project's editable document exactly as it is recorded, read on request:
 * every note and draft with its text, and every revision's lineage with the
 * identity its parent was registered under. */
function EditableDocument({ result, indicators }: { result: RevisionsResult | null; indicators: Indicators }) {
  const recorded = result?.revisions ?? null;
  return (
    <section className="editable-document" aria-label="Editable project document">
      {result === null ? null : result.state === "empty" ? (
        <p className="hint">No notes, drafts or revisions recorded.</p>
      ) : result.state !== "completed" ? (
        <Report indicators={indicators} progress={null} result={result} />
      ) : null}
      {recorded && result?.state === "completed" ? (
        <>
          <h5>
            {recorded.notes.length} {recorded.notes.length === 1 ? "note" : "notes"}
          </h5>
          <ul className="recorded-notes">
            {recorded.notes.map((note) => (
              <li key={note.name}>
                <span className="name">{note.name}</span>
                <span className="badge">{note.subject ? `About ${note.subject}` : "Project draft"}</span>
                <p className="title">{note.title}</p>
                <p className="body">{note.body}</p>
              </li>
            ))}
          </ul>
          <h5>
            {recorded.revisions.length} {recorded.revisions.length === 1 ? "revision" : "revisions"}
          </h5>
          <ul className="recorded-lineage">
            {recorded.revisions.map((revision) => (
              <li key={revision.name}>
                <span className="name">{revision.name}</span>
                <span className="badge">
                  {operationName(revision.operation.name)} of {revision.operation.parent}
                </span>
                <span className="identity">{revision.identity}</span>
                <span className="identity">parent {revision.operation.parent_identity}</span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </section>
  );
}

/** The details a person records about one case, as the edit and registration
 * sheets collect them. */
type CaseDetails = {
  title: string;
  owner: string;
  status: CaseStatus | "";
  interface_version: string;
  tags: string;
  incidents: string;
};

function CaseDetailFields({
  prefix,
  details,
  versions,
  defaultOwner,
  defaultVersion,
  statusRequired,
  onChange,
}: {
  prefix: string;
  details: CaseDetails;
  versions: string[];
  defaultOwner: string;
  defaultVersion: string;
  /** A registered case always has a status; a new registration may take the project's default. */
  statusRequired: boolean;
  onChange: (details: CaseDetails) => void;
}) {
  return (
    <>
      <Field label="Title" htmlFor={`${prefix}-title`}>
        <input
          id={`${prefix}-title`}
          type="text"
          autoFocus
          value={details.title}
          onChange={(event) => onChange({ ...details, title: event.target.value })}
        />
      </Field>
      <div className="fields">
        <Field label="Owner" htmlFor={`${prefix}-owner`}>
          <input
            id={`${prefix}-owner`}
            type="text"
            placeholder={defaultOwner}
            value={details.owner}
            onChange={(event) => onChange({ ...details, owner: event.target.value })}
          />
        </Field>
        <Field label="Status" htmlFor={`${prefix}-status`}>
          <select
            id={`${prefix}-status`}
            value={details.status}
            onChange={(event) => onChange({ ...details, status: event.target.value as CaseStatus | "" })}
          >
            {statusRequired ? null : <option value="">Open (default)</option>}
            {STATUSES.map((status) => (
              <option key={status} value={status}>
                {humanize(status)}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Interface version" htmlFor={`${prefix}-version`}>
          <select
            id={`${prefix}-version`}
            value={details.interface_version}
            onChange={(event) => onChange({ ...details, interface_version: event.target.value })}
          >
            {statusRequired ? null : <option value="">{defaultVersion ? `${defaultVersion} (default)` : "Project default"}</option>}
            {versions.map((version) => (
              <option key={version} value={version}>
                {version}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <Field label="Tags" htmlFor={`${prefix}-tags`} hint="Separate with commas.">
        <input
          id={`${prefix}-tags`}
          type="text"
          value={details.tags}
          onChange={(event) => onChange({ ...details, tags: event.target.value })}
        />
      </Field>
      <Field label="Linked incidents" htmlFor={`${prefix}-incidents`} hint="Separate with commas.">
        <input
          id={`${prefix}-incidents`}
          type="text"
          value={details.incidents}
          onChange={(event) => onChange({ ...details, incidents: event.target.value })}
        />
      </Field>
    </>
  );
}

/** The project: its cases — registered or only present in the folder — its
 * revisions and notes, and its own details. */
export function ProjectPanel({
  root,
  result,
  editable,
  entries,
  busy,
  progress,
  indicators,
  selectedCase,
  onUpdateSettings,
  onReadEditable,
  onRegister,
  onUpdateCase,
  onOpenCase,
}: {
  root: string | null;
  result: ProjectOverviewResult | null;
  /** The editable document as the last request to read it answered. */
  editable: RevisionsResult | null;
  entries: Artifact[];
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  selectedCase: string | null;
  onUpdateSettings: (change: SettingsChange) => Promise<boolean>;
  onRegister: (name: string, registration: CaseRegistration) => void;
  onUpdateCase: (name: string, change: CaseChange) => void;
  onReadEditable: () => void;
  onOpenCase: (name: string) => void;
}) {
  const overview: ProjectOverview | null = result?.overview ?? null;
  const [editing, setEditing] = useState<{ entry: RegisteredCase; details: CaseDetails } | null>(null);
  const [registering, setRegistering] = useState<{ name: string; details: CaseDetails } | null>(null);
  const [settings, setSettings] = useState<{ title: string; owner: string; version: string; declare: string } | null>(null);
  const [sourceOpen, setSourceOpen] = useState(false);

  if (!root) {
    return null;
  }
  if (!overview) {
    // A read or write the project refused is reported where the project would be.
    return result && result.state !== "empty" && result.state !== "completed" ? (
      <Report indicators={indicators} progress={progress} result={result} />
    ) : progress ? (
      <Report indicators={indicators} progress={progress} result={null} />
    ) : null;
  }

  const registered = new Set(overview.cases.map((entry) => entry.name));
  const unregistered = entries.filter((artifact) => artifact.kind === "case" && !registered.has(artifact.name));
  const total = overview.cases.length + unregistered.length;

  return (
    <>
      <section className="project-cases" aria-labelledby="project-cases-title">
        <div className="group-header">
          <h2 id="project-cases-title">
            Cases <span className="count">{total}</span>
          </h2>
        </div>
        <Report indicators={indicators} progress={progress} result={result && result.state !== "completed" && result.state !== "empty" ? result : null} />
        {total === 0 ? (
          <EmptyState title="No cases yet">Import or capture messages to start an investigation.</EmptyState>
        ) : (
          <div className="table-scroll">
            <table className="data-table cases-table">
              <thead>
                <tr>
                  <th scope="col">Case</th>
                  <th scope="col">Status</th>
                  <th scope="col">Owner</th>
                  <th scope="col">Version</th>
                  <th scope="col">Tags</th>
                  <th scope="col">
                    <span className="visually-hidden">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {overview.cases.map((entry) => (
                  <tr key={entry.name} aria-current={selectedCase === entry.name ? "true" : undefined}>
                    <th scope="row">
                      <button type="button" className="row-link" disabled={busy} aria-label={`Open case ${entry.name}`} onClick={() => onOpenCase(entry.name)}>
                        {entry.title || entry.name}
                      </button>
                      <span className="row-sub">{entry.name}</span>
                    </th>
                    <td>
                      <span className={`badge status-${entry.status}`}>
                        <span aria-hidden="true">{indicators.get(entry.status)?.symbol}</span> {humanize(entry.status)}
                      </span>
                      {entry.evidence !== "verified" ? <span className="badge warn">{humanize(entry.evidence)}</span> : null}
                    </td>
                    <td>{entry.owner || "—"}</td>
                    <td>{entry.interface_version || "—"}</td>
                    <td>
                      {entry.tags.length === 0 ? "—" : entry.tags.map((tag) => <span key={tag} className="badge">{tag}</span>)}
                    </td>
                    <td className="row-actions">
                      <button
                        type="button"
                        disabled={busy}
                        aria-label={`Edit ${entry.title || entry.name}`}
                        onClick={() =>
                          setEditing({
                            entry,
                            details: {
                              title: entry.title,
                              owner: entry.owner ?? "",
                              status: entry.status,
                              interface_version: entry.interface_version,
                              tags: entry.tags.join(", "),
                              incidents: entry.incidents.join(", "),
                            },
                          })
                        }
                      >
                        Edit
                      </button>
                    </td>
                  </tr>
                ))}
                {unregistered.map((artifact) => (
                  <tr key={artifact.name} aria-current={selectedCase === artifact.name ? "true" : undefined}>
                    <th scope="row">
                      <button type="button" className="row-link" disabled={busy} aria-label={`Open case ${artifact.name}`} onClick={() => onOpenCase(artifact.name)}>
                        {artifact.name}
                      </button>
                      <span className="row-sub">{humanize(artifact.provenance ?? "")}</span>
                    </th>
                    <td>
                      <span className="badge">Not in project</span>
                    </td>
                    <td>—</td>
                    <td>—</td>
                    <td>—</td>
                    <td className="row-actions">
                      <button
                        type="button"
                        disabled={busy}
                        aria-label={`Add ${artifact.name} to the project`}
                        onClick={() =>
                          setRegistering({
                            name: artifact.name,
                            details: { title: artifact.name, owner: "", status: "", interface_version: "", tags: "", incidents: "" },
                          })
                        }
                      >
                        Add to project…
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {overview.revisions.length > 0 ? (
        <section className="project-revisions" aria-labelledby="project-revisions-title">
          <h2 id="project-revisions-title">
            Revisions <span className="count">{overview.revisions.length}</span>
          </h2>
          <table className="data-table revisions">
            <thead>
              <tr>
                <th scope="col">Revision</th>
                <th scope="col">Made from</th>
                <th scope="col">Evidence</th>
              </tr>
            </thead>
            <tbody>
              {overview.revisions.map((revision) => (
                <tr key={revision.name}>
                  <th scope="row">
                    <button type="button" className="row-link" disabled={busy} aria-label={`Open revision ${revision.name}`} onClick={() => onOpenCase(revision.name)}>
                      {revision.name}
                    </button>
                  </th>
                  <td>
                    {operationName(revision.operation)} of {revision.parent}
                  </td>
                  <td>{humanize(revision.evidence)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}

      {overview.notes.length > 0 ? (
        <section className="project-notes" aria-labelledby="project-notes-title">
          <h2 id="project-notes-title">
            Notes <span className="count">{overview.notes.length}</span>
          </h2>
          <ul className="item-list notes">
            {overview.notes.map((note) => (
              <li key={note.name}>
                <span className="item-main">
                  <span className="item-title">{note.title}</span>
                  {note.subject ? <span className="item-sub">About {note.subject}</span> : null}
                </span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section className="project-details" aria-labelledby="project-details-title">
        <div className="group-header">
          <h2 id="project-details-title">Project</h2>
          <div className="group-aside">
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                setSettings({
                  title: overview.title,
                  owner: overview.default_owner ?? "",
                  version: overview.default_version ?? "",
                  declare: "",
                })
              }
            >
              Edit…
            </button>
          </div>
        </div>
        <dl className="facts">
          <div className="fact">
            <dt>Title</dt>
            <dd>{overview.title}</dd>
          </div>
          <div className="fact">
            <dt>Default owner</dt>
            <dd>{overview.default_owner || "—"}</dd>
          </div>
          <div className="fact">
            <dt>Interface versions</dt>
            <dd>
              {overview.interface_versions.map((version) => (
                <span key={version} className={version === overview.default_version ? "badge accent" : "badge"}>
                  {version}
                  {version === overview.default_version ? " · default" : ""}
                </span>
              ))}
            </dd>
          </div>
          <div className="fact">
            <dt>Folder</dt>
            <dd className="root">{overview.root}</dd>
          </div>
        </dl>
        <details
          className="more"
          open={sourceOpen}
          onToggle={(event) => {
            const open = (event.currentTarget as HTMLDetailsElement).open;
            if (open && !sourceOpen) onReadEditable();
            setSourceOpen(open);
          }}
        >
          <summary>Project document</summary>
          <div className="more-body">{sourceOpen ? <EditableDocument result={editable} indicators={indicators} /> : null}</div>
        </details>
      </section>

      <FormDialog
        open={editing !== null}
        title={editing ? `Edit ${editing.entry.title || editing.entry.name}` : "Edit case"}
        submitLabel="Save"
        busy={busy}
        onClose={() => setEditing(null)}
        onSubmit={() => {
          if (!editing) return;
          const { entry, details } = editing;
          // Only what changed is sent, so an edit changes exactly the members
          // the person named and nothing beside them.
          const change: CaseChange = {};
          if (details.title !== entry.title) change.title = details.title;
          if (details.owner !== (entry.owner ?? "")) change.owner = details.owner;
          if (details.status !== "" && details.status !== entry.status) change.status = details.status;
          if (details.interface_version !== entry.interface_version) change.interface_version = details.interface_version;
          if (list(details.tags).join(",") !== entry.tags.join(",")) change.tags = list(details.tags);
          if (list(details.incidents).join(",") !== entry.incidents.join(",")) change.incidents = list(details.incidents);
          onUpdateCase(entry.name, change);
          setEditing(null);
        }}
      >
        {editing ? (
          <CaseDetailFields
            prefix="case"
            details={editing.details}
            versions={overview.interface_versions}
            defaultOwner={overview.default_owner ?? ""}
            defaultVersion={overview.default_version ?? ""}
            statusRequired
            onChange={(details) => setEditing({ ...editing, details })}
          />
        ) : null}
      </FormDialog>

      <FormDialog
        open={registering !== null}
        title={registering ? `Add ${registering.name} to the project` : "Add to the project"}
        submitLabel="Add to project"
        busy={busy}
        onClose={() => setRegistering(null)}
        onSubmit={() => {
          if (!registering) return;
          const { details } = registering;
          const registration: CaseRegistration = { title: details.title.trim() };
          if (details.owner.trim() !== "") registration.owner = details.owner.trim();
          if (details.status !== "") registration.status = details.status;
          if (details.interface_version !== "") registration.interface_version = details.interface_version;
          registration.tags = list(details.tags);
          registration.incidents = list(details.incidents);
          onRegister(registering.name, registration);
          setRegistering(null);
        }}
      >
        {registering ? (
          <CaseDetailFields
            prefix="register"
            details={registering.details}
            versions={overview.interface_versions}
            defaultOwner={overview.default_owner ?? ""}
            defaultVersion={overview.default_version ?? ""}
            statusRequired={false}
            onChange={(details) => setRegistering({ ...registering, details })}
          />
        ) : null}
      </FormDialog>

      <FormDialog
        open={settings !== null}
        title="Project settings"
        submitLabel="Save"
        busy={busy}
        onClose={() => setSettings(null)}
        onSubmit={() => {
          if (!settings) return;
          // A field left as it was sends nothing, so a settings edit changes
          // only what it names.
          const change: SettingsChange = {};
          if (settings.title !== overview.title) change.title = settings.title;
          if (settings.owner !== (overview.default_owner ?? "")) change.default_owner = settings.owner;
          if (settings.version !== (overview.default_version ?? "")) change.default_interface_version = settings.version;
          const declared = settings.declare.trim();
          if (declared !== "") change.declare_versions = [declared];
          void onUpdateSettings(change).then((stored) => {
            // A refused store keeps everything typed beside the refusal.
            if (stored) setSettings(null);
          });
        }}
        status={result && result.state !== "completed" && result.state !== "empty" ? <Report indicators={indicators} progress={null} result={result} /> : null}
      >
        {settings ? (
          <>
            <Field label="Title" htmlFor="settings-title">
              <input
                id="settings-title"
                type="text"
                autoFocus
                value={settings.title}
                onChange={(event) => setSettings({ ...settings, title: event.target.value })}
              />
            </Field>
            <Field label="Default owner" htmlFor="settings-owner">
              <input
                id="settings-owner"
                type="text"
                value={settings.owner}
                onChange={(event) => setSettings({ ...settings, owner: event.target.value })}
              />
            </Field>
            <Field label="Default interface version" htmlFor="settings-version">
              <select
                id="settings-version"
                value={settings.version}
                onChange={(event) => setSettings({ ...settings, version: event.target.value })}
              >
                <option value="">None</option>
                {overview.interface_versions.map((declared) => (
                  <option key={declared} value={declared}>
                    {declared}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Add an interface version" htmlFor="settings-declare" hint="Versions are never removed; registered cases name them.">
              <input
                id="settings-declare"
                type="text"
                placeholder="2.5.1"
                value={settings.declare}
                onChange={(event) => setSettings({ ...settings, declare: event.target.value })}
              />
            </Field>
          </>
        ) : null}
      </FormDialog>
    </>
  );
}
