import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  applyHubRetention,
  cancelHubAdministrationPreview,
  chooseHubLocalCopy,
  discardEditorDraft,
  editorDrafts,
  exportHubSetup,
  listHubMembers,
  newIntentId,
  openHubConflict,
  postHubLifecycle,
  prepareHubMembership,
  previewHubAdministration,
  previewHubRetention,
  saveHubAudit,
  type EditorDraft,
  type HubAdminMembershipResult,
  type HubAdminRequest,
  type HubAdminResult,
  type HubConflictResult,
  type HubFileType,
  type HubLifecycleCommandRequest,
  type HubMember,
  type HubResource,
  type HubResult,
  type HubRetentionResult,
  type HubTeamResult,
  type MergeChoice,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { DisplayTerm, RETENTION_CHANGES, TEAM_ACTIVITY as TEAM_ACTIVITY_LABELS, TEAM_FILE_TYPES, TEAM_ROLES } from "./display";
import { BackLink, EmptyState, FormDialog, Menu, Modal, ValueRows } from "./layout";
import { OperatorHub } from "./OperatorHub";
import { ReviewSheet } from "./ReviewSheet";
import { ActivityTable, ResourceHistory, TeamTransferOutcome, TransferReview, fileName, revisionLine, teamContext, useCommandIntent } from "./TeamCollaboration";
import { useViewState } from "./viewstate";

type Task = "members" | "retention" | "audit" | "revisions" | "operator" | "hosts";

const TASKS: Record<Task, string> = {
  members: "Members",
  retention: "Retention",
  audit: "Audit log",
  revisions: "Revisions",
  operator: "Operator hub",
  hosts: "Host tasks",
};

/** Settings › Team › Administrator setup: each administration task is its
 * own named page. The team's own tasks appear to its administrators; the
 * hub still decides every one. The operator hub and host tasks need no team
 * sign-in: they are the host operator's. */
export function TeamAdministratorSetup({
  status,
  project,
  team,
  teamName,
  workspace,
  operatorRequest = 0,
  onOperatorConfigured,
  onBack,
  onRead,
  onCreateRevision,
}: {
  status: HubResult | null;
  project: string | null;
  team: HubTeamResult | null;
  teamName: string;
  workspace: string;
  operatorRequest?: number;
  onOperatorConfigured?: ((chosen: boolean) => void) | undefined;
  onBack: () => void;
  onRead: () => Promise<void>;
  onCreateRevision: (resource: string, base: string, source?: string, draft?: string) => void;
}) {
  const [task, setTask] = useViewState<Task | null>("AdministratorSetup.task", null);
  useEffect(() => {
    if (operatorRequest > 0) setTask("operator");
  }, [operatorRequest]); // eslint-disable-line react-hooks/exhaustive-deps
  const signedIn = Boolean(status?.authenticated && project && team?.state === "completed");
  const admin = signedIn && (team?.capabilities.includes("admin") ?? false);
  // Revisions stay reachable offline for the project last opened: a draft is
  // kept on this computer and submitted once signed in again.
  const tasks: Task[] = [...(admin ? (["members", "retention", "audit"] as Task[]) : []), ...(project ? (["revisions"] as Task[]) : []), "operator", "hosts"];
  if (task === null || !tasks.includes(task)) {
    return (
      <>
        <div className="team-header">
          <BackLink label="Team" onBack={onBack} />
          <h2 className="team-name">Administrator setup</h2>
        </div>
        <ul className="task-list" aria-label="Administrator tasks">
          {tasks.map((key) => (
            <li key={key}>
              <button type="button" className="task-row" onClick={() => setTask(key)}>
                {TASKS[key]}
              </button>
            </li>
          ))}
        </ul>
      </>
    );
  }
  return (
    <>
      <div className="team-header">
        <BackLink label="Administrator setup" onBack={() => setTask(null)} />
        <h2 className="team-name">{TASKS[task]}</h2>
      </div>
      {task === "members" && project && team ? <MembersTask project={project} team={team} onRead={onRead} /> : null}
      {task === "retention" && project && team ? <RetentionTask project={project} team={team} onRead={onRead} /> : null}
      {task === "audit" && project && team ? <AuditTask project={project} team={team} onRead={onRead} /> : null}
      {task === "revisions" && project ? (
        <RevisionsTask project={project} team={signedIn ? team : null} teamName={teamName} workspace={workspace} onRead={onRead} onCreateRevision={onCreateRevision} />
      ) : null}
      {task === "operator" ? <OperatorHub request={operatorRequest} {...(onOperatorConfigured ? { onConfigured: onOperatorConfigured } : {})} /> : null}
      {task === "hosts" ? <HostTasks /> : null}
    </>
  );
}

// ---------- Members ----------

type Pending = { subject: string; role: string };
type MemberRow = HubMember & { pending?: string };

function MembersTask({ project, team, onRead }: { project: string; team: HubTeamResult; onRead: () => Promise<void> }) {
  const [members, setMembers] = useState<HubMember[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "add-member" | "change-role" | "remove">(null);
  // Setups handed to the host operator: shown as ready until the members the
  // hub reads show the change installed.
  const [pending, setPending] = useViewState<Pending[]>(`MembersTask.pending.${project}`, []);
  const read = useCallback(async () => {
    const answer = await listHubMembers(project);
    if (answer.state === "completed") {
      setMembers(answer.members);
      setProblem(null);
      setPending((held) => held.filter((entry) => !answer.members.some((member) => member.subject === entry.subject && member.role === entry.role && !member.removed)));
    } else {
      setProblem(answer.reason ?? "The members could not be read.");
    }
  }, [project, setPending]);
  useEffect(() => {
    void read();
  }, [read]);
  const rows: MemberRow[] = [
    ...(members ?? []).map((member) => ({ ...member, ...(pending.find((entry) => entry.subject === member.subject) ? { pending: pending.find((entry) => entry.subject === member.subject)!.role } : {}) })),
    ...pending.filter((entry) => !(members ?? []).some((member) => member.subject === entry.subject)).map((entry) => ({ subject: entry.subject, pending: entry.role })),
  ];
  const member = rows.find((row) => row.subject === selected) ?? null;
  const columns: Column<MemberRow>[] = [
    { key: "subject", header: "Name", priority: 1, minWidth: 12, flex: true, render: (row) => row.subject },
    { key: "role", header: "Role", priority: 2, minWidth: 8, render: (row) => (row.role ? <DisplayTerm map={TEAM_ROLES} code={row.role} /> : "—") },
    {
      key: "status",
      header: "Status",
      priority: 2,
      minWidth: 10,
      render: (row) => (row.pending ? `Setup ready · ${TEAM_ROLES[row.pending as keyof typeof TEAM_ROLES] ?? row.pending}` : row.removed ? "Removed" : "Active"),
    },
  ];
  return (
    <>
      <div className="section-toolbar">
        <button type="button" onClick={() => setSheet("add-member")}>
          Add member
        </button>
        <button type="button" disabled={!member || member.removed || !member.role} onClick={() => setSheet("change-role")}>
          Change role
        </button>
        {member && member.role && !member.removed ? (
          <Menu label="More member actions" items={[{ label: "Remove member", tone: "danger", onSelect: () => setSheet("remove") }]} />
        ) : null}
      </div>
      {problem ? <p role="alert">{problem}</p> : null}
      {members === null && !problem ? (
        <p aria-live="polite">Reading…</p>
      ) : rows.length === 0 ? (
        <EmptyState title="No members" />
      ) : (
        <DataTable label="Members" className="page-table" rows={rows} rowId={(row) => row.subject} rowLabel={(row) => row.subject} columns={columns} selected={selected} onSelect={setSelected} onOpen={setSelected} />
      )}
      <MembershipSheet
        open={sheet === "add-member" || sheet === "change-role"}
        operation={sheet === "change-role" ? "change-role" : "add-member"}
        project={project}
        subject={sheet === "change-role" ? (member?.subject ?? "") : ""}
        role={sheet === "change-role" ? (member?.role ?? "viewer") : "viewer"}
        onClose={() => setSheet(null)}
        onReady={(entry) => setPending((held) => [...held.filter((other) => other.subject !== entry.subject), entry])}
      />
      <RemoveMemberSheet
        open={sheet === "remove" && member !== null}
        project={project}
        subject={member?.subject ?? ""}
        head={team.lifecycle_head}
        onClose={() => setSheet(null)}
        onRemoved={async () => {
          setSheet(null);
          await Promise.all([read(), onRead()]);
        }}
      />
    </>
  );
}

const ROLES = Object.keys(TEAM_ROLES) as (keyof typeof TEAM_ROLES)[];

/** Add member or Change role: prepared over a local copy of the host's
 * access policy and handed to the host operator to install. Nothing here
 * changes anyone's access. */
function MembershipSheet({
  open,
  operation,
  project,
  subject: startSubject,
  role: startRole,
  onClose,
  onReady,
}: {
  open: boolean;
  operation: "add-member" | "change-role";
  project: string;
  subject: string;
  role: string;
  onClose: () => void;
  onReady: (entry: Pending) => void;
}) {
  const [copy, setCopy] = useViewState("MembershipSheet.copy", "");
  const [path, setPath] = useViewState("MembershipSheet.path", "/etc/readmit-hub/access.json");
  const [subject, setSubject] = useState(startSubject);
  const [role, setRole] = useState(startRole);
  const [result, setResult] = useState<HubAdminMembershipResult | null>(null);
  const [exported, setExported] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setSubject(startSubject);
    setRole(startRole);
    setResult(null);
    setExported(null);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const title = operation === "add-member" ? "Add member" : "Change role";
  if (open && result?.state === "completed") {
    return (
      <Modal
        open
        title={title}
        onClose={onClose}
        footer={
          <>
            {exported ? <p className="dialog-status" role="status">{exported}</p> : null}
            <div className="dialog-footer">
              <button type="button" onClick={() => void navigator.clipboard?.writeText(result.command ?? "")}>
                Copy command
              </button>
              <button
                type="button"
                className="primary"
                onClick={() =>
                  void exportHubSetup({ name: result.policy_name ?? "access.json", content: result.policy ?? "" }).then((answer) => {
                    if (answer.state === "completed") {
                      setExported("Setup ready · installation required");
                      onReady({ subject: result.subject ?? subject, role: result.after ?? role });
                    } else if (answer.state !== "cancelled") {
                      setExported(answer.reason ?? "The setup was not exported.");
                    }
                  })
                }
              >
                Export setup
              </button>
            </div>
          </>
        }
      >
        <ValueRows
          label="Change"
          rows={[
            { label: "Person", value: result.subject ?? subject },
            { label: "Project", value: result.project ?? project },
            { label: "Role", value: `${result.before ? (TEAM_ROLES[result.before as keyof typeof TEAM_ROLES] ?? result.before) : "—"} → ${TEAM_ROLES[result.after as keyof typeof TEAM_ROLES] ?? result.after}` },
          ]}
        />
        <HandoffDetails command={result.command ?? ""} prerequisites={result.prerequisites ?? []} touches={result.touches ?? []} untouched={result.does_not_touch ?? []} />
      </Modal>
    );
  }
  return (
    <FormDialog
      open={open}
      title={title}
      submitLabel="Prepare"
      submitDisabled={!copy || !path || subject.trim() === ""}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await prepareHubMembership({ operation, policy_copy: copy, policy_path: path, project, subject: subject.trim(), role });
        if (answer.state !== "completed") return { reason: answer.reason ?? "The change could not be prepared." };
        setResult(answer);
        return null;
      }}
    >
      <LocalCopy id="membership-copy" label="Access policy copy" kind="access-policy" value={copy} onChange={setCopy} />
      <label htmlFor="membership-path">Installed policy on host</label>
      <input id="membership-path" value={path} onChange={(event) => setPath(event.target.value)} />
      <label htmlFor="membership-subject">Person</label>
      <input id="membership-subject" value={subject} readOnly={operation === "change-role"} maxLength={256} onChange={(event) => setSubject(event.target.value)} />
      <label htmlFor="membership-role">Role</label>
      <select id="membership-role" value={role} onChange={(event) => setRole(event.target.value)}>
        {ROLES.map((key) => (
          <option key={key} value={key}>
            {TEAM_ROLES[key]}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** A local copy an administrator task reads, chosen in the host's dialog. */
function LocalCopy({ id, label, kind, value, onChange }: { id: string; label: string; kind: string; value: string; onChange: (path: string) => void }) {
  const choose = async () => {
    const answer = await chooseHubLocalCopy(kind);
    if (answer.state === "completed" && answer.paths?.[0]) onChange(answer.paths[0]);
  };
  const parts = value.split(/[\\/]/);
  return (
    <>
      <label htmlFor={id}>{label}</label>
      <div className="field-with-action">
        <span id={id} title={value}>
          {value ? parts[parts.length - 1] || value : "—"}
        </span>
        <button type="button" onClick={() => void choose()}>
          Choose…
        </button>
      </div>
    </>
  );
}

/** What a host handoff asks of the operator: its command and what must be
 * true, what it changes and what it leaves. */
function HandoffDetails({ command, prerequisites, touches, untouched }: { command: string; prerequisites: string[]; touches: string[]; untouched: string[] }) {
  return (
    <>
      <h3 className="section-heading">Command</h3>
      <pre className="hub-admin-command">{command}</pre>
      {prerequisites.length > 0 ? (
        <>
          <h3 className="section-heading">Before running</h3>
          <ul className="plain-list">
            {prerequisites.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </>
      ) : null}
      {touches.length + untouched.length > 0 ? (
        <>
          <h3 className="section-heading">Affected</h3>
          <ul className="plain-list">
            {[...touches, ...untouched].map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </>
      ) : null}
    </>
  );
}

function RemoveMemberSheet({ open, project, subject, head, onClose, onRemoved }: { open: boolean; project: string; subject: string; head: number; onClose: () => void; onRemoved: () => Promise<void> }) {
  const [reason, setReason] = useState("");
  const intent = useCommandIntent<HubLifecycleCommandRequest>(`${project}.remove.${subject}`, "remove");
  useEffect(() => {
    if (open) setReason("");
  }, [open]);
  return (
    <FormDialog
      open={open}
      title="Remove member"
      submitLabel="Remove"
      tone="danger"
      submitDisabled={reason.trim() === ""}
      onClose={onClose}
      status={<p className="consequence">Removes {subject} from {project}; their downloaded copies stay with them.</p>}
      onSubmit={async () => {
        const decision = { subject, reason: reason.trim() };
        const answer = await intent.send(decision, (id) => [{ project, id, expected: head, kind: "remove-user", resource: "", artifact: "", parents: [], subject, until: "", reason: decision.reason }], postHubLifecycle);
        if (answer?.state !== "completed") return { reason: answer?.reason ?? "The member was not removed." };
        await onRemoved();
        return null;
      }}
    >
      <label htmlFor="remove-reason">Reason</label>
      <textarea id="remove-reason" rows={2} maxLength={2048} value={reason} onChange={(event) => setReason(event.target.value)} />
    </FormDialog>
  );
}

// ---------- Retention ----------

function RetentionTask({ project, team, onRead }: { project: string; team: HubTeamResult; onRead: () => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const columns: Column<HubTeamResult["files"][number]>[] = [
    { key: "name", header: "Name", priority: 1, minWidth: 12, flex: true, render: (row) => fileName(row) },
    { key: "type", header: "Type", priority: 2, minWidth: 8, render: (row) => <DisplayTerm map={TEAM_FILE_TYPES} code={row.type} /> },
    { key: "keep", header: "Keep until", priority: 1, minWidth: 8, render: (row) => (row.retired ? "Retired" : row.keep_until ? new Date(row.keep_until).toLocaleDateString() : "—") },
  ];
  return (
    <>
      <div className="section-toolbar">
        <button type="button" disabled={team.files.length === 0} onClick={() => setEditing(true)}>
          Edit retention
        </button>
      </div>
      {team.files.length === 0 ? (
        <EmptyState title="No files" />
      ) : (
        <DataTable
          label="Retention"
          className="page-table"
          rows={team.files}
          rowId={(row) => row.digest}
          rowLabel={(row) => fileName(row)}
          columns={columns}
          selected={selected}
          onSelect={setSelected}
          onOpen={setSelected}
          checked={checked}
          onCheck={setChecked}
        />
      )}
      <RetentionSheet open={editing} project={project} team={team} checked={[...checked]} onClose={() => setEditing(false)} onApplied={onRead} />
    </>
  );
}

/** Edit retention: keep the project's current files in scope for a while
 * from now. The review shows each file's current and proposed date; a date
 * is never shortened, and files added later are not covered. */
function RetentionSheet({ open, project, team, checked, onClose, onApplied }: { open: boolean; project: string; team: HubTeamResult; checked: string[]; onClose: () => void; onApplied: () => Promise<void> }) {
  const [count, setCount] = useState("1");
  const [unit, setUnit] = useState("years");
  const [scope, setScope] = useState<"all" | "type" | "selected">("all");
  const [type, setType] = useState<HubFileType>("file");
  const [review, setReview] = useState<HubRetentionResult | null>(null);
  const [reason, setReason] = useState("");
  const [applied, setApplied] = useState<HubRetentionResult | null>(null);
  const intent = useRef<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setReview(null);
    setApplied(null);
    setReason("");
    setScope(checked.length > 0 ? "selected" : "all");
    setType(team.files[0]?.type ?? "file");
    intent.current = null;
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const types = [...new Set(team.files.map((file) => file.type))];
  const request = { project, count: Number(count), unit, scope, ...(scope === "type" ? { type } : {}), ...(scope === "selected" ? { digests: checked } : {}) };
  const names = useMemo(() => new Map(team.files.map((file) => [file.digest, fileName(file)])), [team.files]);
  const shown = applied ?? review;
  if (open && shown) {
    const extending = (review?.rows ?? []).filter((row) => row.change === "extends").map((row) => row.digest);
    return (
      <FormDialog
        open
        title={applied?.state === "completed" ? "Retention applied to current files" : applied ? "Retention" : "Review retention"}
        submitLabel={applied ? "Done" : "Save"}
        submitDisabled={!applied && (extending.length === 0 || reason.trim() === "")}
        onClose={onClose}
        secondary={
          applied ? null : (
            <button type="button" onClick={() => setReview(null)}>
              Back
            </button>
          )
        }
        status={
          applied ? (
            applied.state !== "completed" ? <p role="alert">{applied.reason}</p> : null
          ) : (
            <p className="consequence">Current files only; nothing is deleted, and downloaded copies are unaffected.</p>
          )
        }
        onSubmit={async () => {
          if (applied) {
            onClose();
            return null;
          }
          intent.current ??= newIntentId();
          const answer = await applyHubRetention({ request, until: review!.until ?? "", digests: extending, reason: reason.trim(), intent_id: intent.current });
          // Nothing was decided file by file: the review stays, and Save
          // tries the same change again.
          if (answer.rows.length === 0) return { reason: answer.reason ?? "The retention change was not applied." };
          setApplied(answer);
          await onApplied();
          return null;
        }}
      >
        <table className="values-table" aria-label="Retention by file">
          <thead>
            <tr>
              <th scope="col">File</th>
              <th scope="col">Current</th>
              <th scope="col">Proposed</th>
              <th scope="col">Change</th>
            </tr>
          </thead>
          <tbody>
            {shown.rows.map((row) => (
              <tr key={row.digest}>
                <th scope="row">{names.get(row.digest) ?? row.name ?? `Artifact · ${row.digest.slice(0, 7)}`}</th>
                <td>{row.current ? new Date(row.current).toLocaleDateString() : "—"}</td>
                <td>{row.proposed ? new Date(row.proposed).toLocaleDateString() : "—"}</td>
                <td>
                  <DisplayTerm map={RETENTION_CHANGES} code={row.change} />
                  {row.reason ? <span className="row-reason"> · {row.reason}</span> : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {applied ? null : (
          <>
            <label htmlFor="retention-reason">Reason</label>
            <textarea id="retention-reason" rows={2} maxLength={2048} value={reason} onChange={(event) => setReason(event.target.value)} />
          </>
        )}
      </FormDialog>
    );
  }
  return (
    <FormDialog
      open={open}
      title="Edit retention"
      submitLabel="Review"
      submitDisabled={!(Number(count) >= 1) || (scope === "selected" && checked.length === 0)}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await previewHubRetention(request);
        if (answer.state !== "completed") return { reason: answer.reason ?? "No files match." };
        setReview(answer);
        return null;
      }}
    >
      <label htmlFor="retention-count">Keep for</label>
      <div className="field-with-action">
        <input id="retention-count" type="number" min={1} max={1000} value={count} onChange={(event) => setCount(event.target.value)} />
        <select aria-label="Unit" value={unit} onChange={(event) => setUnit(event.target.value)}>
          <option value="days">Days</option>
          <option value="months">Months</option>
          <option value="years">Years</option>
        </select>
      </div>
      <label htmlFor="retention-scope">Files</label>
      <select id="retention-scope" value={scope} onChange={(event) => setScope(event.target.value as "all" | "type" | "selected")}>
        <option value="all">All current files</option>
        <option value="type">Current files of one type</option>
        <option value="selected" disabled={checked.length === 0}>
          Selected files
        </option>
      </select>
      {scope === "type" ? (
        <>
          <label htmlFor="retention-type">Type</label>
          <select id="retention-type" value={type} onChange={(event) => setType(event.target.value as HubFileType)}>
            {types.map((key) => (
              <option key={key} value={key}>
                {TEAM_FILE_TYPES[key]}
              </option>
            ))}
          </select>
        </>
      ) : null}
    </FormDialog>
  );
}

// ---------- Audit log ----------

function AuditTask({ project, team, onRead }: { project: string; team: HubTeamResult; onRead: () => Promise<void> }) {
  const [filter, setFilter] = useState({ actor: "", action: "", item: "", since: "" });
  const [sheet, setSheet] = useState<null | "filter" | "export">(null);
  const [selected, setSelected] = useState<string | null>(null);
  const since = filter.since ? Date.now() - Number(filter.since) * 86_400_000 : 0;
  const rows = team.activity.filter(
    (row) =>
      (!filter.actor || row.actor === filter.actor) &&
      (!filter.action || row.action === filter.action) &&
      (!filter.item || (row.object ?? "").toLowerCase().includes(filter.item.toLowerCase())) &&
      (!since || Date.parse(row.at) >= since),
  );
  return (
    <>
      <div className="section-toolbar">
        <button type="button" onClick={() => setSheet("filter")}>
          Filter
        </button>
        <button type="button" onClick={() => setSheet("export")}>
          Export audit
        </button>
      </div>
      {rows.length === 0 ? <EmptyState title="No entries" /> : <ActivityTable label="Audit log" rows={rows} selected={selected} onSelect={setSelected} />}
      <AuditFilterSheet open={sheet === "filter"} filter={filter} actors={[...new Set(team.activity.map((row) => row.actor))].sort()} onClose={() => setSheet(null)} onApply={(next) => { setFilter(next); setSheet(null); }} />
      <ExportAuditSheet open={sheet === "export"} project={project} head={team.lifecycle_head} onRecorded={onRead} onClose={() => setSheet(null)} />
    </>
  );
}

function AuditFilterSheet({
  open,
  filter,
  actors,
  onClose,
  onApply,
}: {
  open: boolean;
  filter: { actor: string; action: string; item: string; since: string };
  actors: string[];
  onClose: () => void;
  onApply: (filter: { actor: string; action: string; item: string; since: string }) => void;
}) {
  const [draft, setDraft] = useState(filter);
  useEffect(() => {
    if (open) setDraft(filter);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog open={open} title="Filter audit log" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(draft)}>
      <label htmlFor="audit-actor">Person</label>
      <select id="audit-actor" value={draft.actor} onChange={(event) => setDraft({ ...draft, actor: event.target.value })}>
        <option value="">Anyone</option>
        {actors.map((actor) => (
          <option key={actor} value={actor}>
            {actor}
          </option>
        ))}
      </select>
      <label htmlFor="audit-action">Action</label>
      <select id="audit-action" value={draft.action} onChange={(event) => setDraft({ ...draft, action: event.target.value })}>
        <option value="">Any action</option>
        {Object.entries(TEAM_ACTIVITY_LABELS).map(([code, label]) => (
          <option key={code} value={code}>
            {label}
          </option>
        ))}
      </select>
      <label htmlFor="audit-item">Item</label>
      <input id="audit-item" value={draft.item} onChange={(event) => setDraft({ ...draft, item: event.target.value })} />
      <label htmlFor="audit-since">Time</label>
      <select id="audit-since" value={draft.since} onChange={(event) => setDraft({ ...draft, since: event.target.value })}>
        <option value="">Any time</option>
        <option value="1">Last day</option>
        <option value="7">Last 7 days</option>
        <option value="30">Last 30 days</option>
      </select>
    </FormDialog>
  );
}

/** Export audit: records the export with the hub, which answers the exact
 * history at that point, then saves it to a new file the person names. */
function ExportAuditSheet({ open, project, head, onClose, onRecorded }: { open: boolean; project: string; head: number; onClose: () => void; onRecorded: () => Promise<void> }) {
  const [reason, setReason] = useState("");
  const [saved, setSaved] = useState<string | null>(null);
  const [exportReady, setExportReady] = useState(false);
  const intent = useCommandIntent<HubLifecycleCommandRequest>(`${project}.audit`, "audit");
  useEffect(() => {
    if (open) {
      setReason("");
      setSaved(null);
    }
  }, [open]);
  return (
    <FormDialog
      open={open}
      title="Export audit"
      submitLabel={saved ? "Done" : "Export audit"}
      submitDisabled={!saved && !exportReady && reason.trim() === ""}
      onClose={onClose}
      status={saved ? <p role="status">{saved}</p> : <p className="consequence">The exported history includes people's identities.</p>}
      onSubmit={async () => {
        if (saved) {
          onClose();
          return null;
        }
        const decision = { reason: reason.trim() };
        if (!exportReady) {
          const recorded = await intent.send(decision, (id) => [{ project, id, expected: head, kind: "audit-export", resource: "", artifact: "", parents: [], subject: "", until: "", reason: decision.reason }], postHubLifecycle);
          if (recorded?.state !== "completed") return { reason: recorded?.reason ?? "The audit export was not recorded." };
          setExportReady(true);
          await onRecorded();
        }
        const file = await saveHubAudit(project);
        if (file.state === "cancelled") return { reason: "No file was named. Choose a destination to save the recorded export." };
        if (file.state !== "completed") return { reason: file.reason ?? "The audit was not saved." };
        setSaved(`Saved ${file.path ?? ""}`);
        setExportReady(false);
        return null;
      }}
    >
      <label htmlFor="audit-reason">Reason</label>
      <textarea id="audit-reason" rows={2} maxLength={2048} value={reason} disabled={exportReady} onChange={(event) => setReason(event.target.value)} />
    </FormDialog>
  );
}

// ---------- Revisions ----------

type Draft = { id: string; resource: string; base: string; source: string; note: string };

function RevisionsTask({
  project,
  team,
  teamName,
  workspace,
  onRead,
  onCreateRevision,
}: {
  project: string;
  team: HubTeamResult | null;
  teamName: string;
  workspace: string;
  onRead: () => Promise<void>;
  onCreateRevision: (resource: string, base: string, source?: string, draft?: string) => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  const [resolving, setResolving] = useState(false);
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const readDrafts = useCallback(async () => {
    const answer = await editorDrafts();
    setDrafts(
      (answer.drafts ?? [])
        .filter((draft: EditorDraft) => draft.kind === "hub-revision" && draft.identity === project)
        .map((draft: EditorDraft) => {
          const content = draft.content as { resource: string; parent_tips: string[]; local_path: string; note: string };
          return { id: draft.id, resource: content.resource, base: content.parent_tips[0] ?? "", source: content.local_path, note: content.note };
        }),
    );
  }, [project]);
  useEffect(() => {
    void readDrafts();
  }, [readDrafts]);
  const resources = team?.resources ?? [];
  const resource = resources.find((entry) => entry.resource === selected) ?? null;
  // Signed out, a revision can still be drafted on this computer.
  const writes = team === null || team.capabilities.includes("evidence.write");
  const columns: Column<HubResource>[] = [
    { key: "resource", header: "Resource", priority: 1, minWidth: 12, flex: true, render: (row) => row.resource },
    { key: "revisions", header: "Revisions", priority: 3, minWidth: 6, render: (row) => String(row.revisions.length) },
    { key: "status", header: "Status", priority: 2, minWidth: 8, render: (row) => (row.tips.length > 1 ? "Conflict" : "Current") },
  ];
  return (
    <>
      <div className="section-toolbar">
        <button type="button" disabled={!writes} onClick={() => onCreateRevision(resource?.resource ?? "", resource?.tips[0] ?? "")}>
          Create revision
        </button>
        <button type="button" disabled={!team || !resource || resource.tips.length < 2 || !writes} onClick={() => setResolving(true)}>
          Resolve
        </button>
      </div>
      {resources.length === 0 ? (
        <EmptyState title="No revisions" />
      ) : (
        <DataTable label="Resources" className="page-table" rows={resources} rowId={(row) => row.resource} rowLabel={(row) => row.resource} columns={columns} selected={selected} onSelect={setSelected} onOpen={setSelected} />
      )}
      <ResourceHistory resource={resource ?? undefined} />
      {drafts.length > 0 ? (
        <>
          <h3 className="section-heading">Drafts</h3>
          <ul className="plain-list" aria-label="Revision drafts">
            {drafts.map((draft) => (
              <li key={draft.id} className="row-actions">
                <span>
                  {draft.resource} · {draft.note}
                </span>
                <button type="button" disabled={!team || !writes} onClick={() => onCreateRevision(draft.resource, draft.base, draft.source, draft.id)}>
                  Submit revision
                </button>
                <button type="button" className="quiet" onClick={() => void discardEditorDraft(draft.id).then(readDrafts)}>
                  Discard
                </button>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {resource ? <ResolveSheet open={resolving} project={project} teamName={teamName} workspace={workspace} resource={resource.resource} onClose={() => setResolving(false)} onDone={() => void onRead()} /> : null}
    </>
  );
}

/** Resolve: the conflicting revisions side by side — Base, Yours and
 * Current for each part both changed — and one explicit choice for each, or
 * one complete revision where the format has no merge. Save resolution
 * records one new revision naming them all. */
function ResolveSheet({ open, project, teamName, workspace, resource, onClose, onDone }: { open: boolean; project: string; teamName: string; workspace: string; resource: string; onClose: () => void; onDone: () => void }) {
  const [conflict, setConflict] = useState<HubConflictResult | null>(null);
  const [choices, setChoices] = useState<(MergeChoice | "")[]>([]);
  const [whole, setWhole] = useState("");
  const [reason, setReason] = useState("");
  const [reload, setReload] = useState(0);
  useEffect(() => {
    if (!open) return;
    setConflict(null);
    setWhole("");
    setReason("");
    let current = true;
    void openHubConflict({ project, resource }).then((answer) => {
      if (!current) return;
      setConflict(answer);
      setChoices(answer.hunks.filter((hunk) => hunk.conflict).map(() => ""));
    });
    return () => {
      current = false;
    };
  }, [open, reload, project, resource]); // eslint-disable-line react-hooks/exhaustive-deps
  const context = useMemo(() => teamContext(workspace), [workspace]);
  const merging = conflict?.text === true && whole === "";
  const resolution = merging ? { scope: conflict?.scope ?? "", choices: choices.filter((choice): choice is MergeChoice => choice !== "") } : { scope: conflict?.scope ?? "", whole };
  const complete = merging ? choices.every((choice) => choice !== "") : whole !== "";
  const options = useMemo(() => ({ team: { project, resource, resolution } }), [project, resource, JSON.stringify(resolution)]); // eslint-disable-line react-hooks/exhaustive-deps
  let at = -1;
  return (
    <ReviewSheet
      open={open}
      title="Resolve"
      action="team.resolve"
      finalLabel="Save resolution"
      context={context}
      items={noItems}
      options={options}
      prepareKey={JSON.stringify(resolution)}
      canPrepare={conflict?.state === "completed" && complete}
      onPrepared={(review) => {
        if (review.team?.conflict_scope && review.team.conflict_scope !== conflict?.scope) setReload((value) => value + 1);
      }}
      onStale={() => setReload((value) => value + 1)}
      outcome={(result) => <TeamTransferOutcome result={result} />}
      rationale={reason.trim()}
      blocked={() => reason.trim() === ""}
      onClose={onClose}
      onDone={onDone}
      consequence={`Records one new revision of ${resource} in ${teamName}/${project}; no one's revision is overwritten.`}
      fields={
        conflict === null ? (
          <p aria-live="polite">Reading…</p>
        ) : conflict.state !== "completed" ? (
          <p role="alert">{conflict.reason ?? "The revisions could not be read."}</p>
        ) : (
          <>
            <fieldset>
              <legend>Keep</legend>
              {conflict.text ? (
                <label className="check">
                  <input type="radio" name="resolve-whole" checked={whole === ""} onChange={() => setWhole("")} /> Combine changes
                </label>
              ) : null}
              {conflict.tips.map((tip) => (
                <label key={tip.id} className="check">
                  <input type="radio" name="resolve-whole" checked={whole === tip.id} onChange={() => setWhole(tip.id)} /> {tip.id === conflict.yours ? "Yours" : "Current"} · {revisionLine(tip)}
                </label>
              ))}
            </fieldset>
            {merging
              ? conflict.hunks.map((hunk, index) => {
                  if (!hunk.conflict) return null;
                  at += 1;
                  const part = at;
                  return (
                    <fieldset key={index} className="merge-conflict">
                      <legend>Conflict {part + 1}</legend>
                      {(["base", "yours", "current"] as MergeChoice[]).map((side) => (
                        <label key={side} className="merge-side">
                          <input
                            type="radio"
                            name={`conflict-${part}`}
                            checked={choices[part] === side}
                            onChange={() => setChoices((held) => held.map((choice, which) => (which === part ? side : choice)))}
                          />
                          <span className="merge-side-name">{side === "base" ? "Base" : side === "yours" ? "Yours" : "Current"}</span>
                          <pre>{(side === "base" ? hunk.base : side === "yours" ? hunk.yours : hunk.current)?.join("") || "(empty)"}</pre>
                        </label>
                      ))}
                    </fieldset>
                  );
                })
              : null}
            <label htmlFor="resolve-reason">Change</label>
            <textarea id="resolve-reason" rows={2} maxLength={2048} value={reason} onChange={(event) => setReason(event.target.value)} />
          </>
        )
      }
      render={(review) => <TransferReview review={review} teamName={teamName} />}
    />
  );
}

const noItems: never[] = [];

// ---------- Host tasks ----------

type HostTask = HubAdminRequest["operation"];

const HOST_TASKS: { value: HostTask; label: string }[] = [
  { value: "migrate", label: "Migrate metadata" },
  { value: "check", label: "Check readiness" },
  { value: "backup", label: "Create backup" },
  { value: "verify-backup", label: "Verify backup" },
  { value: "restore", label: "Restore backup" },
  { value: "schedule-init", label: "Initialize schedules" },
  { value: "schedule-pin", label: "Pin inputs" },
];

const emptyRequest: HubAdminRequest = {
  operation: "migrate",
  config_copy: "",
  config_path: "/etc/readmit-hub/config.json",
  directory: "",
  local_copy: "",
  operation_policy_copy: "",
  operation_policy_path: "/etc/readmit-hub/operations.json",
  schedule_policy_copy: "",
  schedule_policy_path: "/etc/readmit-hub/schedules.json",
};

/** Host tasks: a reviewed command for each maintenance step the host
 * operator runs. The window reads local copies only; it never runs the
 * command or contacts the host. */
export function HostTasks() {
  const [task, setTask] = useState<HostTask | null>(null);
  return (
    <>
      <ul className="task-list" aria-label="Host tasks">
        {HOST_TASKS.map((entry) => (
          <li key={entry.value}>
            <button type="button" className="task-row" onClick={() => setTask(entry.value)}>
              {entry.label}
            </button>
          </li>
        ))}
      </ul>
      <HostTaskSheet task={task} onClose={() => setTask(null)} />
    </>
  );
}

function HostTaskSheet({ task, onClose }: { task: HostTask | null; onClose: () => void }) {
  const [request, setRequest] = useViewState<HubAdminRequest>("HostTasks.request", emptyRequest);
  const [result, setResult] = useState<HubAdminResult | null>(null);
  const [running, setRunning] = useState(false);
  const [exported, setExported] = useState<string | null>(null);
  useEffect(() => {
    setResult(null);
    setExported(null);
  }, [task]);
  const change = <K extends keyof HubAdminRequest>(name: K, value: HubAdminRequest[K]) => {
    setResult(null);
    setRequest((held) => ({ ...held, [name]: value }));
  };
  const label = HOST_TASKS.find((entry) => entry.value === task)?.label ?? "Host task";
  if (task && result?.state === "completed" && result.command) {
    const setup = [`# ${label}`, ...(result.prerequisites ?? []).map((item) => `# ${item}`), result.command, ""].join("\n");
    return (
      <Modal
        open
        title={label}
        onClose={onClose}
        footer={
          <>
            {exported ? <p className="dialog-status" role="status">{exported}</p> : null}
            <div className="dialog-footer">
              <button type="button" onClick={() => void navigator.clipboard?.writeText(result.command ?? "")}>
                Copy command
              </button>
              <button
                type="button"
                className="primary"
                onClick={() =>
                  void exportHubSetup({ name: `${task}.sh`, content: setup }).then((answer) => {
                    if (answer.state !== "cancelled") setExported(answer.state === "completed" ? `Saved ${answer.path ?? ""}` : (answer.reason ?? "The setup was not exported."));
                  })
                }
              >
                Export setup
              </button>
            </div>
          </>
        }
      >
        {result.local_result ? <ValueRows label="Local check" rows={[{ label: "Local copy", value: result.local_result }]} /> : null}
        <HandoffDetails command={result.command} prerequisites={result.prerequisites ?? []} touches={result.touches ?? []} untouched={result.does_not_touch ?? []} />
      </Modal>
    );
  }
  const directory = task === "backup" || task === "verify-backup" || task === "restore" || task === "schedule-pin";
  const local = task === "verify-backup" || task === "restore" || task === "schedule-pin";
  const schedule = task === "schedule-init";
  return (
    <FormDialog
      open={task !== null}
      title={label}
      submitLabel="Preview command"
      busy={running}
      onClose={onClose}
      status={
        running ? (
          <span className="review-running">
            <span className="spinner" aria-hidden="true" />
            <span>Checking local copies…</span>
            <button type="button" className="quiet" onClick={() => void cancelHubAdministrationPreview()}>
              Stop
            </button>
          </span>
        ) : result && result.state !== "completed" ? (
          <p role="alert">{result.reason}</p>
        ) : undefined
      }
      onSubmit={async () => {
        if (!task) return null;
        setRunning(true);
        const answer = await previewHubAdministration({ ...request, operation: task }).finally(() => setRunning(false));
        setResult(answer);
        return answer.state === "completed" ? null : { reason: answer.reason ?? "The command could not be previewed." };
      }}
    >
      <LocalCopy id="host-config-copy" label="Configuration copy" kind="config" value={request.config_copy} onChange={(path) => change("config_copy", path)} />
      <label htmlFor="host-config-path">Configuration on host</label>
      <input id="host-config-path" value={request.config_path} onChange={(event) => change("config_path", event.target.value)} />
      {local ? (
        <LocalCopy
          id="host-local-copy"
          label={task === "schedule-pin" ? "Test copy" : "Backup copy"}
          kind={task === "schedule-pin" ? "test" : "backup"}
          value={request.local_copy}
          onChange={(path) => change("local_copy", path)}
        />
      ) : null}
      {directory ? (
        <>
          <label htmlFor="host-directory">{task === "schedule-pin" ? "Test on host" : "Backup folder on host"}</label>
          <input id="host-directory" value={request.directory} onChange={(event) => change("directory", event.target.value)} />
        </>
      ) : null}
      {schedule ? (
        <>
          <LocalCopy id="host-operation-copy" label="Operation policy copy" kind="operation-policy" value={request.operation_policy_copy} onChange={(path) => change("operation_policy_copy", path)} />
          <label htmlFor="host-operation-path">Operation policy on host</label>
          <input id="host-operation-path" value={request.operation_policy_path} onChange={(event) => change("operation_policy_path", event.target.value)} />
          <LocalCopy id="host-schedule-copy" label="Schedule policy copy" kind="schedule-policy" value={request.schedule_policy_copy} onChange={(path) => change("schedule_policy_copy", path)} />
          <label htmlFor="host-schedule-path">Schedule policy on host</label>
          <input id="host-schedule-path" value={request.schedule_policy_path} onChange={(event) => change("schedule_policy_path", event.target.value)} />
        </>
      ) : null}
    </FormDialog>
  );
}
