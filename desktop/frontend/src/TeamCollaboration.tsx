import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  chooseHubLocalCopy,
  downloadHubFile,
  downloadHubSummary,
  listHubReviewers,
  listWholeCatalog,
  postHubReview,
  postHubSupportReview,
  prepareAction,
  readHubSupportSummary,
  reconcileTeamTransfer,
  searchHubReviews,
  withdrawReview,
  saveHubOfflineDraft,
  type ActionReview,
  type HubActivity,
  type HubReviewCommandRequest,
  type HubReviewItem,
  type HubRevision,
  type HubSupportReviewRequest,
  type HubSupportSummaryResult,
  type HubTeamFile,
  type HubTeamResult,
  type HubTransferResult,
  type RequestContext,
  type ReviewedActionResult,
  type SuiteComparison,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { DisplayTerm, TEAM_ACTIVITY, TEAM_FILE_TYPES, TEAM_REVIEW_STATUSES } from "./display";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, BackLink, type SubmitFailure } from "./layout";
import { ReviewSheet } from "./ReviewSheet";
import { TaskTabs } from "./TaskTabs";
import { listDate } from "./Projects";
import { sizeText } from "./Storage";
import { ChangeTables } from "./Suites";
import { useViewState } from "./viewstate";

type Tab = "activity" | "files" | "reviews";

/** The window's request context for a team transfer: the open project, which
 * a team transfer does not read, and no generation of its own. */
export function teamContext(workspace: string): () => RequestContext {
  return () => ({ project: workspace, generation: 0 });
}

/** A new hub command's identity: the hub's grammar of lowercase letters,
 * digits and hyphens. */
function commandId(prefix: string): string {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return `${prefix}-${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

/** Whether the hub settled a command: recorded it, or refused it for a
 * reason that does not change on a retry. Anything else — no answer, a
 * transport failure — leaves the command pending. */
function settled(answer: { state: string; resolved?: boolean | undefined }): boolean {
  return answer.state === "completed" || answer.resolved === true;
}

/** One deliberate decision's commands: built once, on the first click, under
 * a new identity, and sent again exactly as they were while the hub has not
 * settled them, so a retry never records the decision twice. Once the hub
 * settles them the next click is a new decision; nothing here allocates a new
 * identity on its own to get past a refusal. The pending commands outlive the
 * sheet, as unsent work does. */
export function useCommandIntent<C>(key: string, prefix: string) {
  const [held, setHeld] = useViewState<{ decision: string; commands: C[] } | null>(`CommandIntent.${key}`, null);
  const current = useRef(held);
  current.current = held;
  return {
    async send<A extends { state: string; reason?: string | undefined; resolved?: boolean | undefined }>(decision: unknown, build: (id: string) => C[], post: (command: C) => Promise<A>): Promise<A | null> {
      const text = JSON.stringify(decision);
      if (current.current && current.current.decision !== text) {
        throw new Error("The previous request is unresolved. Retry that decision before changing it.");
      }
      const entry = current.current?.decision === text ? current.current : { decision: text, commands: build(commandId(prefix)) };
      current.current = entry;
      setHeld(entry);
      let answer: A | null = null;
      for (const command of entry.commands) {
        answer = await post(command);
        if (answer.state !== "completed") break;
      }
      if (answer && settled(answer)) {
        current.current = null;
        setHeld(null);
      }
      return answer;
    },
  };
}

/** A file's name, or the fallback the project's records allow: never a name
 * invented from its bytes. */
export function fileName(file: HubTeamFile): string {
  return file.name || `Artifact · ${file.digest.slice(0, 7)}`;
}

/** A team project's Activity, Files and Reviews, read through the signed-in
 * session. `team` is the project's metadata the Team page read; `onRead`
 * asks for it again after a change. */
export function TeamCollaboration({
  project,
  teamName,
  team,
  workspace = "",
  entries = [],
  onRead,
  onCreateRevision,
}: {
  project: string;
  teamName: string;
  team: HubTeamResult;
  workspace?: string;
  entries?: { name: string; kind: string }[];
  onRead: () => Promise<void>;
  onCreateRevision: (resource: string, base: string) => void;
}) {
  const [tab, setTab] = useViewState<Tab>("TeamCollaboration.tab", "activity");
  return (
    <section className="team-project" aria-label={`${project} activity`}>
      <TaskTabs<Tab>
        label="Team views"
        id={`team-${project}`}
        selected={tab}
        onSelect={setTab}
        tabs={[
          { key: "activity", label: "Activity" },
          { key: "files", label: "Files" },
          { key: "reviews", label: "Reviews" },
        ]}
      >
        {tab === "activity" ? <ActivityView project={project} team={team} /> : null}
        {tab === "files" ? (
          <FilesView project={project} teamName={teamName} team={team} workspace={workspace} onRead={onRead} onCreateRevision={onCreateRevision} />
        ) : null}
        {tab === "reviews" ? <ReviewsView project={project} team={team} workspace={workspace} entries={entries} onRead={onRead} /> : null}
      </TaskTabs>
    </section>
  );
}

// ---------- Activity ----------

type ActivityFilter = { toMe: boolean; action: string; actor: string };

function ActivityView({ project, team }: { project: string; team: HubTeamResult }) {
  const [sheet, setSheet] = useState<null | "filter" | "search">(null);
  const [filter, setFilter] = useViewState<ActivityFilter>(`TeamCollaboration.filter.${project}`, { toMe: false, action: "", actor: "" });
  const [found, setFound] = useState<{ text: string; rows: HubActivity[] } | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const shown = (found?.rows ?? team.activity).filter(
    (row) => (!filter.toMe || row.to_me) && (!filter.action || row.action === filter.action) && (!filter.actor || row.actor === filter.actor),
  );
  const actors = [...new Set(team.activity.map((row) => row.actor))].sort();
  return (
    <>
      <div className="section-toolbar">
        <button type="button" onClick={() => setSheet("filter")}>
          Filter
        </button>
        <button type="button" onClick={() => setSheet("search")}>
          Search
        </button>
        {found ? (
          <button type="button" className="quiet" onClick={() => setFound(null)}>
            Clear search
          </button>
        ) : null}
      </div>
      {shown.length === 0 ? (
        <EmptyState title={found ? "Nothing matches" : filter.toMe ? "Nothing addressed to you" : "No activity"} />
      ) : (
        <ActivityTable rows={shown} selected={selected} onSelect={setSelected} />
      )}
      <ActivityFilterSheet open={sheet === "filter"} filter={filter} actors={actors} onClose={() => setSheet(null)} onApply={(next) => { setFilter(next); setSheet(null); }} />
      <SearchSheet
        open={sheet === "search"}
        onClose={() => setSheet(null)}
        onSearch={async (text) => {
          const answer = await searchHubReviews({ project, after: 0, text, evidence: "" });
          if (answer.state !== "completed") return { reason: answer.reason ?? "The search did not complete." };
          setFound({
            text,
            rows: (answer.events ?? [])
              .map((event) => ({
                key: `found-${event.sequence}`,
                actor: event.actor,
                action: event.kind as HubActivity["action"],
                at: event.at,
                text: event.kind.startsWith("support") ? "" : event.text,
                to_me: event.recipient === team.me,
              }))
              .reverse(),
          });
          setSheet(null);
          return null;
        }}
      />
    </>
  );
}

export function ActivityTable({ rows, selected, onSelect, label = "Activity" }: { rows: HubActivity[]; selected: string | null; onSelect: (key: string) => void; label?: string }) {
  const columns: Column<HubActivity>[] = [
    { key: "action", header: "Action", priority: 1, minWidth: 10, flex: true, render: (row) => <DisplayTerm map={TEAM_ACTIVITY} code={row.action} /> },
    { key: "object", header: "Item", priority: 2, minWidth: 8, flex: true, render: (row) => row.object || "—" },
    { key: "actor", header: "Person", priority: 3, minWidth: 8, render: (row) => row.actor },
    { key: "at", header: "Time", priority: 4, minWidth: 7, render: (row) => listDate(row.at) },
  ];
  return (
    <DataTable
      label={label}
      className="page-table"
      rows={rows}
      rowId={(row) => row.key}
      rowLabel={(row) => `${TEAM_ACTIVITY[row.action] ?? row.action} ${row.object ?? ""}`}
      columns={columns}
      selected={selected}
      onSelect={onSelect}
      onOpen={onSelect}
    />
  );
}

function ActivityFilterSheet({ open, filter, actors, onClose, onApply }: { open: boolean; filter: ActivityFilter; actors: string[]; onClose: () => void; onApply: (filter: ActivityFilter) => void }) {
  const [draft, setDraft] = useState(filter);
  useEffect(() => {
    if (open) setDraft(filter);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog open={open} title="Filter activity" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(draft)}>
      <label htmlFor="activity-show">Show</label>
      <select id="activity-show" value={draft.toMe ? "me" : "all"} onChange={(event) => setDraft({ ...draft, toMe: event.target.value === "me" })}>
        <option value="all">All activity</option>
        <option value="me">Addressed to me</option>
      </select>
      <label htmlFor="activity-action">Action</label>
      <select id="activity-action" value={draft.action} onChange={(event) => setDraft({ ...draft, action: event.target.value })}>
        <option value="">Any action</option>
        {Object.entries(TEAM_ACTIVITY).map(([code, label]) => (
          <option key={code} value={code}>
            {label}
          </option>
        ))}
      </select>
      <label htmlFor="activity-actor">Person</label>
      <select id="activity-actor" value={draft.actor} onChange={(event) => setDraft({ ...draft, actor: event.target.value })}>
        <option value="">Anyone</option>
        {actors.map((actor) => (
          <option key={actor} value={actor}>
            {actor}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

function SearchSheet({ open, onClose, onSearch }: { open: boolean; onClose: () => void; onSearch: (text: string) => Promise<SubmitFailure | null> }) {
  const [text, setText] = useState("");
  return (
    <FormDialog open={open} title="Search activity" submitLabel="Search" submitDisabled={text.trim() === ""} onClose={onClose} onSubmit={() => onSearch(text.trim())}>
      <label htmlFor="activity-search">Text</label>
      <input id="activity-search" type="search" maxLength={256} value={text} onChange={(event) => setText(event.target.value)} />
    </FormDialog>
  );
}

// ---------- Files ----------

function FilesView({
  project,
  teamName,
  team,
  workspace,
  onRead,
  onCreateRevision,
}: {
  project: string;
  teamName: string;
  team: HubTeamResult;
  workspace: string;
  onRead: () => Promise<void>;
  onCreateRevision: (resource: string, base: string) => void;
}) {
  const [selected, setSelected] = useViewState<string | null>(`TeamCollaboration.file.${project}`, null);
  const [source, setSource] = useState<string | null>(null);
  const [details, setDetails] = useState(false);
  const [transfer, setTransfer] = useState<HubTransferResult | null>(null);
  const [choosing, setChoosing] = useState(false);
  const file = team.files.find((entry) => entry.digest === selected) ?? null;
  const writes = team.capabilities.includes("evidence.write");
  const context = useMemo(() => teamContext(workspace), [workspace]);
  const options = useMemo(() => ({ team: { project, source: source ?? "" } }), [project, source]);
  const columns: Column<HubTeamFile>[] = [
    { key: "name", header: "Name", priority: 1, minWidth: 12, flex: true, render: (row) => fileName(row) },
    { key: "type", header: "Type", priority: 2, minWidth: 8, render: (row) => <DisplayTerm map={TEAM_FILE_TYPES} code={row.type} /> },
    { key: "added_by", header: "Added by", priority: 3, minWidth: 8, render: (row) => row.added_by || "—" },
    { key: "added_at", header: "Date", priority: 4, minWidth: 7, render: (row) => listDate(row.added_at) },
  ];
  const upload = async () => {
    setChoosing(true);
    const chosen = await chooseHubLocalCopy("file").finally(() => setChoosing(false));
    if (chosen.state === "completed" && chosen.paths?.[0]) setSource(chosen.paths[0]);
  };
  const download = async () => {
    if (!file) return;
    setTransfer(null);
    const answer = await downloadHubFile({ project, digest: file.digest, name: file.name ? file.name : "" });
    if (answer.state !== "cancelled") setTransfer(answer);
  };
  return (
    <>
      <div className="section-toolbar">
        <button type="button" disabled={!writes || choosing} onClick={() => void upload()}>
          Upload
        </button>
        <button type="button" disabled={!file || file.retired} onClick={() => void download()}>
          Download
        </button>
        {file ? (
          <Menu
            label="More file actions"
            items={[
              { label: "Details", onSelect: () => setDetails(true) },
              ...(file.resource && writes
                ? [{ label: "Create revision", onSelect: () => onCreateRevision(file.resource!, team.resources.find((entry) => entry.resource === file.resource)?.tips[0] ?? "") }]
                : []),
            ]}
          />
        ) : null}
      </div>
      {transfer ? (
        <p role={transfer.state === "completed" ? "status" : "alert"} className="transfer-line">
          {transfer.state === "completed" ? `Saved ${transfer.path ?? ""}` : (transfer.reason ?? "The download did not complete.")}
        </p>
      ) : null}
      {team.files.length === 0 ? (
        <EmptyState title="No files" />
      ) : (
        <DataTable
          label="Files"
          className="page-table"
          rows={team.files}
          rowId={(row) => row.digest}
          rowLabel={(row) => fileName(row)}
          columns={columns}
          selected={selected}
          onSelect={setSelected}
          onOpen={(id) => {
            setSelected(id);
            setDetails(true);
          }}
        />
      )}
      <Modal open={details && file !== null} title={file ? fileName(file) : "File"} onClose={() => setDetails(false)}>
        {file ? (
          <ValueRows
            label="File details"
            rows={[
              { label: "Type", value: <DisplayTerm map={TEAM_FILE_TYPES} code={file.type} /> },
              { label: "Size", value: sizeText(file.size) },
              { label: "Added by", value: file.added_by || "—" },
              { label: "Added", value: file.added_at ? new Date(file.added_at).toLocaleString() : "—" },
              { label: "Keep until", value: file.keep_until ? new Date(file.keep_until).toLocaleDateString() : "—" },
              ...(file.retired ? [{ label: "Status", value: "Retired" }] : []),
              { label: "SHA-256", value: <code className="digest">{file.digest}</code> },
            ]}
          />
        ) : null}
        {file?.resource ? <ResourceHistory resource={team.resources.find((entry) => entry.resource === file.resource)} /> : null}
      </Modal>
      <ReviewSheet
        open={source !== null}
        title="Upload"
        action="team.upload"
        finalLabel="Upload"
        context={context}
        items={noItems}
        options={options}
        prepareKey={source ?? ""}
        onClose={() => setSource(null)}
        onDone={() => void onRead()}
        render={(review) => <TransferReview review={review} teamName={teamName} />}
        consequence={`Uploads ${source ? baseName(source) : "the file"} to ${teamName}/${project}.`}
        outcome={(result) => <TeamTransferOutcome result={result} />}
      />
    </>
  );
}

const noItems: never[] = [];

/** A resource's published revisions, newest first, with its current ones
 * marked. */
export function ResourceHistory({ resource }: { resource: { revisions: HubRevision[]; tips: string[] } | undefined }) {
  if (!resource) return null;
  return (
    <>
      <h3 className="section-heading">History</h3>
      <ul className="plain-list revision-history" aria-label="History">
        {resource.revisions
          .slice()
          .reverse()
          .map((revision) => (
            <li key={revision.id}>
              {revisionLine(revision)}
              {resource.tips.includes(revision.id) ? <span className="row-reason"> · Current</span> : null}
            </li>
          ))}
      </ul>
    </>
  );
}

function baseName(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

/** What a transfer will send, as its review shows it. */
export function TransferReview({ review, teamName }: { review: ActionReview; teamName: string }) {
  const [status, setStatus] = useState("");
  const shown = review.team;
  if (!shown) return null;
  const tips = shown.tips ?? [];
  return (
    <>
    {shown.pending_operation ? <button type="button" onClick={async () => {
      const answer = await reconcileTeamTransfer(shown.pending_operation!);
      setStatus(answer.outcome === "completed" ? "Revision confirmed. Close and review again." : answer.reason ?? "The revision is still unconfirmed.");
    }}>Check status</button> : null}
    {status ? <p role="status">{status}</p> : null}
    <ValueRows
      label="Transfer"
      rows={[
        ...(shown.name ? [{ label: "Name", value: shown.name }] : []),
        ...(shown.name ? [{ label: "Type", value: fileType(shown.name) }] : []),
        { label: "Size", value: sizeText(shown.size) },
        { label: "Team", value: shown.team || teamName },
        { label: "Project", value: shown.project },
        ...(shown.resource ? [{ label: "Resource", value: shown.resource }] : []),
        ...(shown.base ? [{ label: "Base", value: revisionLine(shown.base) }] : []),
        ...(tips.length > 0 ? [{ label: tips.length > 1 ? "Current revisions" : "Current revision", value: tips.map(revisionLine).join("; ") }] : []),
      ]}
    />
    </>
  );
}

/** Reads only metadata to resolve a lost transfer reply. It never uploads
 * bytes or repeats a lifecycle command. */
export function TeamTransferOutcome({ result, onConfirmed }: { result: ReviewedActionResult; onConfirmed?: (result: ReviewedActionResult) => void }) {
  const [status, setStatus] = useState("");
  const [checking, setChecking] = useState(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const transfer = result.team;
  if (!transfer) return null;
  if (result.outcome === "completed") return <p role="status">{transfer.revision ? "Revision recorded" : "Uploaded"}</p>;
  return <div>
    {transfer.uploaded ? <p>File uploaded</p> : null}
    {result.outcome === "uncertain" ? <button type="button" disabled={checking} onClick={async () => {
      setChecking(true);
      const answer = await reconcileTeamTransfer(result.operation ?? "").finally(() => { if (alive.current) setChecking(false); });
      if (!alive.current) return;
      if (answer.outcome === "completed" && answer.state === "completed") {
        setStatus(answer.team?.revision ? "Revision recorded" : "Uploaded");
        onConfirmed?.(answer);
      } else setStatus(answer.reason ?? "The transfer is still unconfirmed.");
    }}>{checking ? "Checking…" : "Check status"}</button> : null}
    {status ? <p role="status">{status}</p> : null}
  </div>;
}

export function revisionLine(revision: HubRevision): string {
  return `${revision.actor} · ${listDate(revision.at)}${revision.reason ? ` · ${revision.reason}` : ""}`;
}

function fileType(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toUpperCase() : "File";
}

// ---------- Reviews ----------

function ReviewsView({
  project,
  team,
  workspace,
  entries,
  onRead,
}: {
  project: string;
  team: HubTeamResult;
  workspace: string;
  entries: { name: string; kind: string }[];
  onRead: () => Promise<void>;
}) {
  const [selected, setSelected] = useViewState<string | null>(`TeamCollaboration.review.${project}`, null);
  const [open, setOpen] = useViewState<string | null>(`TeamCollaboration.openReview.${project}`, null);
  const [sheet, setSheet] = useState<null | "request" | "policy">(null);
  const item = team.reviews.find((entry) => entry.id === open) ?? null;
  if (item) {
    return <ReviewDetail project={project} team={team} item={item} workspace={workspace} onBack={() => setOpen(null)} onRead={onRead} />;
  }
  const columns: Column<HubReviewItem>[] = [
    { key: "item", header: "Item", priority: 1, minWidth: 12, flex: true, render: (row) => reviewTitle(row) },
    { key: "requested_by", header: "Requested by", priority: 2, minWidth: 8, render: (row) => row.requested_by },
    { key: "updated", header: "Updated", priority: 3, minWidth: 7, render: (row) => listDate(row.updated) },
    { key: "status", header: "Status", priority: 2, minWidth: 8, render: (row) => <DisplayTerm map={TEAM_REVIEW_STATUSES} code={row.status} /> },
  ];
  const writes = team.capabilities.includes("evidence.write");
  const bundles = entries.filter((entry) => entry.kind === "support").map((entry) => entry.name);
  const policies = entries.filter((entry) => entry.kind === "sharing-policy").map((entry) => entry.name);
  return (
    <>
      <div className="section-toolbar">
        <button type="button" disabled={!writes || (!workspace && bundles.length === 0)} onClick={() => setSheet("request")}>
          Request review
        </button>
        {team.capabilities.includes("admin") ? (
          <button type="button" className="quiet" disabled={policies.length === 0} onClick={() => setSheet("policy")}>
            Publish policy
          </button>
        ) : null}
      </div>
      {team.reviews.length === 0 ? (
        <EmptyState title="No reviews" />
      ) : (
        <DataTable
          label="Reviews"
          className="page-table"
          rows={team.reviews}
          rowId={(row) => row.id}
          rowLabel={reviewTitle}
          columns={columns}
          selected={selected}
          onSelect={setSelected}
          onOpen={(id) => {
            setSelected(id);
            setOpen(id);
          }}
        />
      )}
      <RequestReviewSheet
        open={sheet === "request"}
        project={project}
        workspace={workspace}
        bundles={bundles}
        onClose={() => setSheet(null)}
        onDone={async () => {
          setSheet(null);
          await onRead();
        }}
      />
      <PublishPolicySheet
        open={sheet === "policy"}
        project={project}
        workspace={workspace}
        policies={policies}
        version={team.activity.filter((row) => row.action === "support-policy").length + 1}
        onClose={() => setSheet(null)}
        onDone={async () => {
          setSheet(null);
          await onRead();
        }}
      />
    </>
  );
}

export function reviewTitle(item: HubReviewItem): string {
  if (item.support) return "Support summary";
  if (item.item) return item.version ? `${item.item} · Version ${item.version}` : item.item;
  return "Test release";
}

/** One review: the changed version or summary it asks about, who asked, the
 * discussion, and the decisions the signed-in person may make. */
function ReviewDetail({
  project,
  team,
  item,
  workspace,
  onBack,
  onRead,
}: {
  project: string;
  team: HubTeamResult;
  item: HubReviewItem;
  workspace: string;
  onBack: () => void;
  onRead: () => Promise<void>;
}) {
  const [sheet, setSheet] = useState<null | "approve" | "changes" | "comment">(null);
  const [changes, setChanges] = useState<SuiteComparison | null | "unavailable">(null);
  const [summary, setSummary] = useState<HubSupportSummaryResult | null>(null);
  const [saved, setSaved] = useState<HubTransferResult | null>(null);
  const [reason, setReason] = useState("");
  const context = useMemo(() => teamContext(workspace), [workspace]);
  const suite = item.suite;
  const items = useMemo(() => (suite ? [suite] : []), [suite]);
  const open = item.status === "requested" && item.to_me;
  // The version the review asks about is read from the open project, and a
  // support summary from the one document the review names; neither is a
  // download of anything else.
  useEffect(() => {
    let current = true;
    if (item.support) {
      void readHubSupportSummary({ project, digest: item.evidence }).then((answer) => current && setSummary(answer));
    } else if (suite) {
      void prepareAction({ context: context(), action: "suite.approve-release", items: [suite] }).then((answer) => {
        if (answer.review?.token) void withdrawReview(answer.review.token);
        if (current) setChanges(answer.review?.suite_approval?.comparison ?? "unavailable");
      });
    } else {
      setChanges("unavailable");
    }
    return () => {
      current = false;
    };
  }, [item.id]); // eslint-disable-line react-hooks/exhaustive-deps
  const approvable = open && (item.support ? summary?.state === "completed" : suite !== undefined && changes !== "unavailable");
  return (
    <div className="object-page team-review">
      <div className="object-header">
        <BackLink label="Reviews" onBack={onBack} />
        <h2>Review</h2>
        <div className="page-actions">
          {open && !item.support ? (
            <button type="button" onClick={() => setSheet("changes")}>
              Request changes
            </button>
          ) : null}
          <button type="button" onClick={() => setSheet("comment")}>
            Comment
          </button>
          {open ? (
            <button type="button" className="primary" disabled={!approvable} onClick={() => setSheet("approve")}>
              {item.support ? "Approve summary" : "Approve"}
            </button>
          ) : null}
          {item.support && item.status === "approved" ? (
            <button
              type="button"
              onClick={() =>
                void downloadHubSummary({ project, digest: item.evidence }).then((answer) => {
                  if (answer.state !== "cancelled") setSaved(answer);
                })
              }
            >
              Download summary
            </button>
          ) : null}
        </div>
      </div>
      <p className="object-subtitle">
        {reviewTitle(item)} · <DisplayTerm map={TEAM_REVIEW_STATUSES} code={item.status} />
      </p>
      <ValueRows
        label="Request"
        rows={[
          { label: "Requested by", value: item.requested_by },
          { label: "Reviewer", value: item.recipient },
          { label: "Requested", value: new Date(item.requested).toLocaleString() },
          ...(item.reason ? [{ label: "Reason", value: item.reason }] : []),
        ]}
      />
      {saved ? (
        <p role={saved.state === "completed" ? "status" : "alert"}>{saved.state === "completed" ? `Saved ${saved.path ?? ""}` : (saved.reason ?? "The summary was not saved.")}</p>
      ) : null}
      <h3 className="section-heading">{item.support ? "Summary" : "Changes"}</h3>
      {item.support ? (
        summary === null ? (
          <p aria-live="polite">Reading…</p>
        ) : summary.state !== "completed" ? (
          <p role="alert">{summary.reason ?? "The summary cannot be read."}</p>
        ) : (
          <ValueRows
            label="Summary"
            rows={[
              { label: "Source", value: SUMMARY_SOURCES[summary.source_kind ?? ""] ?? summary.source_kind ?? "—" },
              { label: "Outcome", value: SUMMARY_OUTCOMES[summary.outcome ?? ""] ?? summary.outcome ?? "—" },
              {
                label: "Sharing policy",
                value: item.policy_version ? `Version ${item.policy_version}${item.policy_current ? "" : " · Replaced"}` : "—",
              },
            ]}
          />
        )
      ) : changes === null ? (
        <p aria-live="polite">Reading…</p>
      ) : changes === "unavailable" ? (
        <p>Not in the open project</p>
      ) : changes.state === "completed" && (changes.tests.length > 0 || changes.changes.length > 0) ? (
        <ChangeTables comparison={changes} />
      ) : (
        <p>No changes</p>
      )}
      {item.discussion.length > 0 ? (
        <>
          <h3 className="section-heading">Discussion</h3>
          <ul className="plain-list discussion">
            {item.discussion.map((entry, index) => (
              <li key={index}>
                <span className="discussion-meta">
                  {entry.actor} · <DisplayTerm map={TEAM_ACTIVITY} code={entry.kind} /> · {listDate(entry.at)}
                </span>
                <span>{entry.text}</span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {suite && !item.support ? (
        <ReviewSheet
          open={sheet === "approve"}
          title="Approve"
          action="suite.approve-release"
          finalLabel="Approve"
          context={context}
          items={items}
          rationale={reason.trim()}
          blocked={() => reason.trim() === ""}
          onClose={() => setSheet(null)}
          onDone={() => void onRead()}
          consequence="Records your approval of this exact version."
          fields={
            <>
              <label htmlFor="team-approve-reason">Reason</label>
              <textarea id="team-approve-reason" rows={2} maxLength={1024} value={reason} onChange={(event) => setReason(event.target.value)} />
            </>
          }
          render={(review) => (review.suite_approval?.comparison ? <ChangeTables comparison={review.suite_approval.comparison} /> : null)}
        />
      ) : null}
      {item.support ? (
        <DecisionSheet
          open={sheet === "approve"}
          title="Approve summary"
          submitLabel="Approve summary"
          project={project}
          team={team}
          kind="support-approval"
          item={item}
          requireText={false}
          consequence="Allows this summary to be downloaded under the current sharing policy."
          onClose={() => setSheet(null)}
          onRead={onRead}
        />
      ) : null}
      <DecisionSheet
        open={sheet === "changes"}
        title="Request changes"
        submitLabel="Request changes"
        project={project}
        team={team}
        kind="change-request"
        item={item}
        requireText
        textLabel="Reason"
        onClose={() => setSheet(null)}
        onRead={onRead}
      />
      <DecisionSheet
        open={sheet === "comment"}
        title="Comment"
        submitLabel="Comment"
        project={project}
        team={team}
        kind="comment"
        item={item}
        requireText
        textLabel="Comment"
        onClose={() => setSheet(null)}
        onRead={onRead}
      />
    </div>
  );
}

const SUMMARY_SOURCES: Record<string, string> = { "retained-packet": "Retained packet", "portable-review": "Portable review", "derived-review": "Derived review" };
const SUMMARY_OUTCOMES: Record<string, string> = { pass: "Passed", assertion_failure: "Check failed", execution_error: "Run error", "reviewed-extract-only": "Reviewed extract" };

/** One decision on a review, recorded once under the signed-in identity: the
 * command's identity is allocated on the first click and kept while it is
 * retried, and a history that moved on is reported, read again, and needs a
 * fresh decision. */
function DecisionSheet({
  open,
  title,
  submitLabel,
  project,
  team,
  kind,
  item,
  requireText,
  textLabel,
  consequence,
  onClose,
  onRead,
}: {
  open: boolean;
  title: string;
  submitLabel: string;
  project: string;
  team: HubTeamResult;
  kind: "comment" | "change-request" | "support-approval";
  item: HubReviewItem;
  requireText: boolean;
  textLabel?: string;
  consequence?: string;
  onClose: () => void;
  onRead: () => Promise<void>;
}) {
  const [text, setText] = useState("");
  const intent = useCommandIntent<HubReviewCommandRequest>(`${project}.${item.id}.${kind}`, kind === "comment" ? "comment" : kind === "change-request" ? "changes" : "approve");
  useEffect(() => {
    if (open) setText("");
  }, [open]);
  return (
    <FormDialog
      open={open}
      title={title}
      submitLabel={submitLabel}
      submitDisabled={requireText && text.trim() === ""}
      dirty={text.trim() !== ""}
      onClose={onClose}
      status={consequence ? <p className="consequence">{consequence}</p> : undefined}
      onSubmit={async () => {
        // A comment is one remark on the review; a decision answers every
        // request the review is, each as its own command, in order.
        const requests = kind === "comment" ? item.requests.slice(0, 1) : item.requests;
        const words = kind === "support-approval" ? "support" : text.trim();
        const answer = await intent.send(
          { kind, words, requests: requests.map((request) => request.id) },
          (id) =>
            requests.map((request, index) => ({
              project,
              id: requests.length > 1 ? `${id}-${index + 1}` : id,
              expected: team.review_head + index,
              kind,
              evidence: request.evidence,
              parent: request.id,
              recipient: "",
              text: words,
              release: kind === "comment" ? "" : request.release,
            })),
          postHubReview,
        );
        if (!answer) return { reason: "This was not recorded." };
        if (answer.state !== "completed") {
          if (/conflict/i.test(answer.reason ?? "")) {
            await onRead();
            return { reason: "Someone else changed this project meanwhile. Look at it again before deciding." };
          }
          return { reason: answer.reason ?? "This was not recorded." };
        }
        onClose();
        await onRead();
        return null;
      }}
    >
      {textLabel ? (
        <>
          <label htmlFor={`team-${kind}-text`}>{textLabel}</label>
          <textarea id={`team-${kind}-text`} rows={3} maxLength={2048} value={text} onChange={(event) => setText(event.target.value)} />
        </>
      ) : (
        <ValueRows label="Decision" rows={[{ label: "Item", value: reviewTitle(item) }, { label: "Requested by", value: item.requested_by }]} />
      )}
    </FormDialog>
  );
}

/** Request review: a suite version of the open project through its reviewed
 * request, or a published support summary; either asks one of the
 * project's reviewers. */
function RequestReviewSheet({
  open,
  project,
  workspace,
  bundles,
  onClose,
  onDone,
}: {
  open: boolean;
  project: string;
  workspace: string;
  bundles: string[];
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const [what, setWhat] = useState<"suite" | "support">("suite");
  const [suite, setSuite] = useState("");
  const [bundle, setBundle] = useState("");
  const [reviewer, setReviewer] = useState("");
  const [reason, setReason] = useState("");
  const [suites, setSuites] = useState<{ id: string; name: string; revision?: string }[]>([]);
  const [reviewers, setReviewers] = useState<string[] | null>(null);
  const intent = useCommandIntent<HubSupportReviewRequest>(`${project}.request`, "request");
  const context = useMemo(() => teamContext(workspace), [workspace]);
  useEffect(() => {
    if (!open) return;
    setWhat(workspace ? "suite" : "support");
    setReason("");
    setBundle(bundles[0] ?? "");
    let current = true;
    void (async () => {
      const [people, catalog] = await Promise.all([
        listHubReviewers(project),
        workspace ? listWholeCatalog({ context: context(), kind: "suite", filter: {} }) : Promise.resolve(null),
      ]);
      if (!current) return;
      const names = people.state === "completed" ? people.members.map((member) => member.subject) : [];
      setReviewers(names);
      setReviewer(names[0] ?? "");
      const listed = catalog?.state === "completed" ? (catalog.page?.items ?? []).map((entry) => ({ id: entry.ref.id, name: entry.name, ...(entry.ref.revision ? { revision: entry.ref.revision } : {}) })) : [];
      setSuites(listed);
      setSuite(listed[0]?.id ?? "");
    })();
    return () => {
      current = false;
    };
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const chosen = suites.find((entry) => entry.id === suite);
  const items = useMemo(() => (chosen ? [{ kind: "suite" as const, id: chosen.id, ...(chosen.revision ? { revision: chosen.revision } : {}) }] : []), [chosen]);
  const options = useMemo(() => ({ suite_approval: { reviewer } }), [reviewer]);
  const recipient = (
    <>
      <label htmlFor="team-request-what">Item</label>
      <select id="team-request-what" value={what} onChange={(event) => setWhat(event.target.value as "suite" | "support")}>
        {workspace ? <option value="suite">Suite version</option> : null}
        {bundles.length > 0 ? <option value="support">Support summary</option> : null}
      </select>
      {what === "suite" ? (
        <>
          <label htmlFor="team-request-suite">Suite</label>
          <select id="team-request-suite" value={suite} onChange={(event) => setSuite(event.target.value)}>
            {suites.length === 0 ? <option value="">No suites</option> : null}
            {suites.map((entry) => (
              <option key={entry.id} value={entry.id}>
                {entry.name}
              </option>
            ))}
          </select>
        </>
      ) : (
        <>
          <label htmlFor="team-request-bundle">Summary</label>
          <select id="team-request-bundle" value={bundle} onChange={(event) => setBundle(event.target.value)}>
            {bundles.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </>
      )}
      <label htmlFor="team-request-reviewer">Reviewer</label>
      <select id="team-request-reviewer" value={reviewer} onChange={(event) => setReviewer(event.target.value)}>
        {reviewers === null ? <option value="">Reading…</option> : reviewers.length === 0 ? <option value="">No reviewers</option> : null}
        {(reviewers ?? []).map((subject) => (
          <option key={subject} value={subject}>
            {subject}
          </option>
        ))}
      </select>
    </>
  );
  if (what === "suite") {
    return (
      <ReviewSheet
        open={open}
        title="Request review"
        action="suite.request-review"
        finalLabel="Request review"
        context={context}
        items={items}
        options={options}
        prepareKey={`${suite}|${reviewer}`}
        canPrepare={reviewer !== "" && items.length > 0}
        rationale={reason.trim()}
        blocked={() => reason.trim() === ""}
        onClose={onClose}
        onDone={() => void onDone()}
        consequence="Asks the reviewer through the team."
        fields={
          <>
            {recipient}
            <label htmlFor="team-request-reason">Reason</label>
            <textarea id="team-request-reason" rows={2} maxLength={1024} value={reason} onChange={(event) => setReason(event.target.value)} />
          </>
        }
        render={(review) => (review.suite_approval?.comparison ? <ChangeTables comparison={review.suite_approval.comparison} /> : null)}
      />
    );
  }
  return (
    <FormDialog
      open={open}
      title="Request approval"
      submitLabel="Request approval"
      submitDisabled={!bundle || !reviewer}
      onClose={onClose}
      status={<p className="consequence">Uploads this summary to {project} and asks the reviewer.</p>}
      onSubmit={async () => {
        const request = { project, workspace, entry: bundle, kind: "support-request", recipient: reviewer };
        const answer = await intent.send(request, (id) => [{ ...request, id }], postHubSupportReview);
        if (answer?.state !== "completed") return { reason: answer?.reason ?? "The request was not recorded." };
        await onDone();
        return null;
      }}
    >
      {recipient}
    </FormDialog>
  );
}

/** Publish policy: the exact sharing policy this project's summaries are
 * judged under from now on. It approves no summary. */
function PublishPolicySheet({
  open,
  project,
  workspace,
  policies,
  version,
  onClose,
  onDone,
}: {
  open: boolean;
  project: string;
  workspace: string;
  policies: string[];
  version: number;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const [policy, setPolicy] = useState("");
  const intent = useCommandIntent<HubSupportReviewRequest>(`${project}.policy`, "policy");
  useEffect(() => {
    if (open) setPolicy(policies[0] ?? "");
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open={open}
      title="Publish policy"
      submitLabel="Publish policy"
      submitDisabled={!policy}
      onClose={onClose}
      status={<p className="consequence">Earlier summary approvals stop applying; no summary is approved.</p>}
      onSubmit={async () => {
        const request = { project, workspace, entry: policy, kind: "support-policy", recipient: "" };
        const answer = await intent.send(request, (id) => [{ ...request, id }], postHubSupportReview);
        if (answer?.state !== "completed") return { reason: answer?.reason ?? "The policy was not published." };
        await onDone();
        return null;
      }}
    >
      <label htmlFor="team-policy">Sharing policy</label>
      <select id="team-policy" value={policy} onChange={(event) => setPolicy(event.target.value)}>
        {policies.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
      </select>
      <ValueRows label="Publication" rows={[{ label: "Project", value: project }, { label: "Version", value: String(version) }]} />
    </FormDialog>
  );
}

// ---------- Revisions ----------

/** Create revision or Submit revision: a chosen file as the next revision of
 * a resource, continuing the base it names, reviewed against the resource as
 * it stands now. Save draft keeps it on this computer only. */
export function RevisionSheet({
  open,
  project,
  teamName,
  workspace,
  resource: startResource,
  base: startBase,
  source: startSource = "",
  resources,
  onClose,
  onDone,
}: {
  open: boolean;
  project: string;
  teamName: string;
  workspace: string;
  resource: string;
  base: string;
  source?: string;
  resources: { resource: string; tips: string[]; revisions: HubRevision[] }[];
  onClose: () => void;
  onDone: (result: ReviewedActionResult) => void;
}) {
  const [resource, setResource] = useState(startResource);
  const [base, setBase] = useState(startBase);
  const [source, setSource] = useState(startSource);
  const [reason, setReason] = useState("");
  const [draft, setDraft] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setResource(startResource);
    setBase(startBase);
    setSource(startSource);
    setReason("");
    setDraft(null);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const known = resources.find((entry) => entry.resource === resource);
  const context = useMemo(() => teamContext(workspace), [workspace]);
  const options = useMemo(() => ({ team: { project, source, resource, base } }), [project, source, resource, base]);
  const choose = useCallback(async () => {
    const chosen = await chooseHubLocalCopy("file");
    if (chosen.state === "completed" && chosen.paths?.[0]) setSource(chosen.paths[0]);
  }, []);
  return (
    <ReviewSheet
      open={open}
      title={startSource ? "Submit revision" : "Create revision"}
      action="team.revision"
      finalLabel="Submit revision"
      context={context}
      items={noItems}
      options={options}
      prepareKey={`${source}|${resource}|${base}`}
      canPrepare={source !== "" && resource !== ""}
      rationale={reason.trim()}
      blocked={() => reason.trim() === ""}
      onClose={onClose}
      onDone={onDone}
      outcome={(result) => <TeamTransferOutcome result={result} onConfirmed={onDone} />}
      consequence={`Uploads the file to ${teamName}/${project} as the next revision of ${resource || "the resource"}.`}
      fields={
        <>
          <label htmlFor="team-revision-file">File</label>
          <div className="field-with-action">
            <span id="team-revision-file">{source ? baseName(source) : "—"}</span>
            <button type="button" onClick={() => void choose()}>
              Choose…
            </button>
          </div>
          <label htmlFor="team-revision-resource">Resource</label>
          <input
            id="team-revision-resource"
            value={resource}
            readOnly={startResource !== ""}
            maxLength={64}
            onChange={(event) => setResource(event.target.value.trim())}
          />
          {known ? (
            <>
              <label htmlFor="team-revision-base">Base</label>
              <select id="team-revision-base" value={base} onChange={(event) => setBase(event.target.value)}>
                {known.revisions
                  .slice()
                  .reverse()
                  .map((revision) => (
                    <option key={revision.id} value={revision.id}>
                      {revisionLine(revision)}
                    </option>
                  ))}
              </select>
            </>
          ) : null}
          <label htmlFor="team-revision-reason">Change</label>
          <textarea id="team-revision-reason" rows={2} maxLength={2048} value={reason} onChange={(event) => setReason(event.target.value)} />
          <div className="field-with-action">
            <button
              type="button"
              className="quiet"
              disabled={!source || !resource || !workspace}
              onClick={() =>
                void saveHubOfflineDraft({
                  workspace,
                  project,
                  resource,
                  parent_tips: base ? [base] : [],
                  local_path: source,
                  expected_head: 0,
                  note: reason.trim() || "Revision draft",
                }).then((answer) => setDraft(answer.state === "completed" ? "Draft saved on this computer" : (answer.reason ?? "The draft was not saved.")))
              }
            >
              Save draft
            </button>
            {draft ? <span role="status">{draft}</span> : null}
          </div>
        </>
      }
      render={(review) => <TransferReview review={review} teamName={teamName} />}
    />
  );
}
