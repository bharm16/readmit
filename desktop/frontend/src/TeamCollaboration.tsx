import { useEffect, useState } from "react";
import {
  listHubReviews,
  postHubReview,
  postHubSupportReview,
  listHubNotifications,
  searchHubReviews,
  searchHubNotifications,
  listHubLifecycle,
  postHubLifecycle,
  downloadHubExport,
  explainHubCustody,
  saveHubOfflineDraft,
  saveHubAudit,
  type HubReviewsResult,
  type HubLifecycleResult,
  type HubLifecycleCommandRequest,
  type HubLifecycleEventView,
  type HubResult,
  type HubTransferResult,
  type EditorDraftsResult,
  type Artifact,
} from "./bindings";
import { useLifecycle } from "./lifecycle";
import { TaskTabs } from "./TaskTabs";

type Props = {
  project: string;
  workspace?: string;
  entries?: Artifact[];
  capabilities?: string[] | undefined;
  /** Told the project, resource and revision state an offline draft needs,
   * so the hub panel can keep offering local retention after sign-out. */
  onRevisionContext?: (context: RevisionContext) => void;
};

/** What an offline revision draft records about where it branched from. */
export interface RevisionContext {
  project: string;
  resource: string;
  tips: string[];
  head: number;
}

type Task = "reviews" | "notifications" | "revisions" | "support" | "administration";

/** A new command ID in the hub's grammar: lowercase letters, digits and
 * hyphens, at most 64 characters. Each deliberate new command gets its own. */
function newCommandId(prefix: string): string {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return `${prefix}-${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

/** One task's command intent. The ID is allocated when a command is first
 * submitted and kept while the same command is retried — the same ID with the
 * same payload, which the hub answers as a replay. Any change to what the
 * command says, or a completed command followed by a new one, allocates a new
 * ID; nothing reuses another task's ID. */
function useIntent(prefix: string) {
  const [intent, setIntent] = useState<{ id: string; payload: string } | null>(null);
  const [recorded, setRecorded] = useState<string | null>(null);
  return {
    pending: intent?.id ?? null,
    recorded,
    /** The ID for this payload: the pending one when it is a retry. */
    idFor(payload: unknown): string {
      const key = JSON.stringify(payload);
      if (intent && intent.payload === key) return intent.id;
      const id = newCommandId(prefix);
      setIntent({ id, payload: key });
      return id;
    },
    /** A completed command is done: the next one is new. */
    completed(id: string) {
      setIntent(null);
      setRecorded(id);
    },
  };
}

/** The command ID of one task, where a person can read it without it being
 * a field to fill in. */
function CommandDetails({ intent }: { intent: { pending: string | null; recorded: string | null } }) {
  return (
    <details className="hub-command-details">
      <summary>Details</summary>
      <dl>
        <dt>Command ID</dt>
        <dd>
          {intent.pending ? (
            <>
              <code>{intent.pending}</code> (not yet recorded; a retry sends it again)
            </>
          ) : intent.recorded ? (
            <>
              <code>{intent.recorded}</code> (recorded; the next action gets a new ID)
            </>
          ) : (
            "A new ID is assigned when you submit."
          )}
        </dd>
      </dl>
    </details>
  );
}

function recordedLine(event: HubLifecycleEventView): string {
  const target = event.resource ?? event.subject ?? event.artifact ?? "";
  return `Recorded ${event.kind} by ${event.actor}@${event.issuer}${target ? ` for ${target}` : ""} · event #${event.sequence} · command ${event.command_id}`;
}

/** The event head a history read reported. The facade leaves a head of 0 out,
 * so a completed read of an empty history is event 0; any other read without
 * a head reported none, and a missing or refused head never reads as a
 * complete history. */
function eventHead(result: HubLifecycleResult): number | undefined {
  if (result.head !== undefined) return result.head;
  return result.state === "completed" && (result.events ?? []).length === 0 ? 0 : undefined;
}

export function TeamCollaboration({ project, workspace = "", entries = [], capabilities = [], onRevisionContext }: Props) {
  const [task, setTask] = useState<Task>("reviews");
  const [reviews, setReviews] = useState<HubReviewsResult | null>(null);
  const [notifications, setNotifications] = useState<HubReviewsResult | null>(null);
  const [lifecycle, setLifecycle] = useState<HubLifecycleResult | null>(null);
  const [custody, setCustody] = useState<HubResult | null>(null);
  const actions = useLifecycle<"working">();
  const busy = actions.running !== null;
  const [message, setMessage] = useState<string | null>(null);

  // A search of the history or of this person's notifications: what to look
  // for, as the hub's query contract carries it, and what the hub found.
  const [queryText, setQueryText] = useState("");
  const [queryEvidence, setQueryEvidence] = useState("");
  const [queryAfter, setQueryAfter] = useState("");
  const [queryProblem, setQueryProblem] = useState<string | null>(null);
  const [found, setFound] = useState<{ scope: "history" | "notifications"; result: HubReviewsResult } | null>(null);

  // The review decision: each kind shows and sends only its own members.
  const [reviewKind, setReviewKind] = useState("comment");
  const [evidence, setEvidence] = useState("");
  const [recipient, setRecipient] = useState("");
  const [release, setRelease] = useState("");
  const [parent, setParent] = useState("");
  const [texts, setTexts] = useState<Record<string, string>>({ comment: "Evidence-linked comment" });
  const reviewIntent = useIntent("review");

  // Revisions: a new revision or a resolve of every current tip.
  const [revisionKind, setRevisionKind] = useState<"revision" | "resolve">("revision");
  const [resource, setResource] = useState("");
  const [revisionArtifact, setRevisionArtifact] = useState("");
  const [parents, setParents] = useState("");
  const [revisionReason, setRevisionReason] = useState("");
  const revisionIntent = useIntent("revision");
  const [revisionWrite, setRevisionWrite] = useState<HubLifecycleResult | null>(null);

  // Administration: access, retention and audit export, each its own command.
  const [removeSubject, setRemoveSubject] = useState("");
  const [removeReason, setRemoveReason] = useState("");
  const [confirmingRemoval, setConfirmingRemoval] = useState(false);
  const accessIntent = useIntent("access");
  const [accessWrite, setAccessWrite] = useState<HubLifecycleResult | null>(null);
  const [retentionKind, setRetentionKind] = useState<"retention" | "retire">("retention");
  const [retentionArtifact, setRetentionArtifact] = useState("");
  const [until, setUntil] = useState("");
  const [retentionReason, setRetentionReason] = useState("");
  const [confirmingRetire, setConfirmingRetire] = useState(false);
  const retentionIntent = useIntent("retention");
  const [retentionWrite, setRetentionWrite] = useState<HubLifecycleResult | null>(null);
  const [auditReason, setAuditReason] = useState("");
  const auditIntent = useIntent("audit");
  const [audit, setAudit] = useState<HubLifecycleResult | null>(null);
  const [auditSaved, setAuditSaved] = useState<HubTransferResult | null>(null);
  // A history read after a recorded write that failed: the write stands.
  const [refreshProblem, setRefreshProblem] = useState<string | null>(null);

  // The sharing journey: the policy and bundle entries of the open workspace,
  // the reviewer asked to approve, and the summary digest a request or an
  // approval named — the digest the hub's export route serves under that
  // chain. The privacy panel's local approval inputs are separate deliberate
  // acts and are never filled from here.
  const sharingPolicies = entries.filter((entry) => entry.kind === "sharing-policy").map((entry) => entry.name);
  const supportBundles = entries.filter((entry) => entry.kind === "support").map((entry) => entry.name);
  const [policyEntry, setPolicyEntry] = useState("");
  const [bundleEntry, setBundleEntry] = useState("");
  const [supportRecipient, setSupportRecipient] = useState("");
  const supportIntent = useIntent("support");
  const [supportReview, setSupportReview] = useState<HubReviewsResult | null>(null);
  const [exportDigest, setExportDigest] = useState("");
  const [exportDestination, setExportDestination] = useState("");
  const [exportResult, setExportResult] = useState<HubTransferResult | null>(null);

  // The hub panel keeps the last known project, resource and revision state
  // for an offline draft after sign-out; telling it asks nothing of the hub.
  const knownTips = lifecycle?.tips?.[resource];
  const knownHead = lifecycle?.head ?? 0;
  useEffect(() => {
    onRevisionContext?.({ project, resource, tips: knownTips ?? [], head: knownHead });
  }, [onRevisionContext, project, resource, knownTips, knownHead]);

  async function run<T>(work: () => Promise<T>, apply: (value: T) => void) {
    await actions.run("working", async () => {
      setMessage(null);
      apply(await work());
    });
  }

  function supportAct(kind: "support-policy" | "support-request" | "support-approval") {
    const request = {
      project,
      workspace,
      entry: kind === "support-policy" ? policyEntry : bundleEntry,
      kind,
      recipient: kind === "support-request" ? supportRecipient.trim() : "",
    };
    const id = supportIntent.idFor(request);
    return run(() => postHubSupportReview({ ...request, id }), (res) => {
      setSupportReview(res);
      if (res.state === "completed") {
        supportIntent.completed(id);
        if (kind !== "support-policy") setExportDigest(res.events?.[0]?.evidence ?? "");
      }
    });
  }

  // Searches are asked of the hub only when the person searches. A sequence
  // that is not a whole number is refused here; every other part of the
  // query is the application's and the hub's to judge.
  function search(scope: "history" | "notifications") {
    const after = queryAfter.trim();
    if (after !== "" && !/^\d{1,9}$/.test(after)) {
      setQueryProblem("Search after a sequence number: a whole number, 0 or more.");
      setFound(null);
      return;
    }
    setQueryProblem(null);
    const request = { project, after: after === "" ? 0 : Number(after), text: queryText, evidence: queryEvidence.trim() };
    void run(
      () => (scope === "history" ? searchHubReviews(request) : searchHubNotifications(request)),
      (result) => setFound({ scope, result }),
    );
  }

  function readLifecycle() {
    void run(
      () => listHubLifecycle(project),
      (result) => {
        setLifecycle(result);
        if (result.state === "completed") setRefreshProblem(null);
      },
    );
  }

  /** Records one lifecycle command, then reads the history again. The write
   * is shown as the hub answered it; a history read that fails afterwards is
   * reported separately and never recasts the recorded write as refused. */
  function postLifecycle(
    request: Omit<HubLifecycleCommandRequest, "id" | "project" | "expected">,
    intent: ReturnType<typeof useIntent>,
    settle: (result: HubLifecycleResult) => void,
  ) {
    const command = { project, expected: lifecycle?.head ?? 0, ...request };
    const id = intent.idFor(command);
    void run(
      async () => {
        const written = await postHubLifecycle({ ...command, id });
        if (written.state !== "completed" || request.kind === "audit-export") return { written, history: null };
        return { written, history: await listHubLifecycle(project) };
      },
      ({ written, history }) => {
        settle(written);
        if (written.state !== "completed") {
          setMessage(written.reason ?? "lifecycle refused");
          return;
        }
        intent.completed(id);
        if (history?.state === "completed") {
          setLifecycle(history);
          setRefreshProblem(null);
        } else if (history) {
          setRefreshProblem(history.reason ?? "The revision history could not be read again.");
        }
      },
    );
  }

  const reviewActions: Record<string, string> = {
    comment: "Post comment",
    assignment: "Assign",
    "review-request": "Request review",
    approval: "Approve review",
  };
  const textLabel: Record<string, string> = {
    comment: "Comment",
    assignment: "Assignment note",
    "review-request": "Rationale",
    approval: "Rationale",
  };
  const usesRecipient = reviewKind === "comment" || reviewKind === "assignment" || reviewKind === "review-request";
  const usesRelease = reviewKind === "review-request" || reviewKind === "approval";
  const usesParent = reviewKind === "comment" || reviewKind === "approval";
  const reviewText = texts[reviewKind] ?? "";
  const tipsOf = lifecycle?.tips ?? {};
  const supportKind = supportReview?.state === "completed" ? supportReview.events?.[0]?.kind : undefined;

  const searchForm = (scope: "history" | "notifications") => (
    <form
      className="hub-collab-form"
      aria-label="Search team activity"
      onSubmit={(e) => {
        e.preventDefault();
        search(scope);
      }}
    >
      <h4>Search team activity</h4>
      <p className="hub-team-note">
        {scope === "history"
          ? "The hub searches what it recorded for this project. Nothing is asked until you search."
          : "The hub searches only what is addressed to you. Nothing is asked until you search."}{" "}
        The text is matched literally, not ranked.
      </p>
      <label>
        Search text
        <input value={queryText} onChange={(e) => setQueryText(e.target.value)} disabled={busy} />
      </label>
      <label>
        Evidence SHA-256
        <input value={queryEvidence} onChange={(e) => setQueryEvidence(e.target.value)} disabled={busy} aria-describedby="hub-search-evidence-help" />
      </label>
      <span className="hub-team-note" id="hub-search-evidence-help">
        The whole 64-character digest.
      </span>
      <label>
        After event number
        <input value={queryAfter} inputMode="numeric" onChange={(e) => setQueryAfter(e.target.value)} disabled={busy} aria-describedby="hub-search-after-help" />
      </label>
      <span className="hub-team-note" id="hub-search-after-help">
        Only events after this number are searched.
      </span>
      <div className="hub-actions">
        <button type="submit" disabled={busy}>
          {scope === "history" ? "Search history" : "Search notifications"}
        </button>
      </div>
      {queryProblem ? <p role="alert">{queryProblem}</p> : null}
      {found && found.scope === scope ? (
        <div className="hub-collab-block">
          <h5>
            {found.result.state === "completed"
              ? `${found.scope === "history" ? "History" : "Notifications"} matching: ${(found.result.events ?? []).length} (head ${found.result.head ?? 0})`
              : `${found.scope === "history" ? "History" : "Notification"} search did not complete`}
          </h5>
          {found.result.state !== "completed" ? (
            <p role="status">{found.result.reason ?? "The search could not be completed."}</p>
          ) : (found.result.events ?? []).length === 0 ? (
            <p>Nothing recorded matches this search.</p>
          ) : null}
          <ul>
            {(found.result.events ?? []).map((event) => (
              <li key={`s-${event.command_id}-${event.sequence}`}>
                #{event.sequence} <strong>{event.kind}</strong> by {event.actor}@{event.issuer} · evidence{" "}
                {event.evidence.slice(0, 12)}… — {event.text}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </form>
  );

  return (
    <section className="hub-team-section" aria-label="Team collaboration">
      <h3>Team reviews</h3>
      <p className="hub-team-note">
        Project {project}. Decisions use the authenticated hub identity. A local reviewer name cannot approve.
        Stale heads and changed grants require a renewed action. Membership and IdP assignment
        stay under customer-admin access policy — this panel does not edit raw policy JSON.
      </p>
      <div className="hub-actions">
        <button type="button" disabled={busy} onClick={() => void run(() => explainHubCustody(), setCustody)}>
          Download limits
        </button>
      </div>
      {custody && (
        <aside className="hub-custody-warning" role="note">
          <strong>Custody:</strong> {custody.custody_warning}
          {custody.reason ? <p>{custody.reason}</p> : null}
        </aside>
      )}

      <TaskTabs<Task>
        label="Team tasks"
        id={`hub-team-${project}`}
        selected={task}
        onSelect={setTask}
        tablistClass="hub-team-tabs"
        panelClass="hub-team-task"
        tabs={[
          { key: "reviews", label: "Reviews" },
          { key: "notifications", label: "Notifications" },
          { key: "revisions", label: "Revisions" },
          { key: "support", label: "Support approvals" },
          { key: "administration", label: "Administration" },
        ]}
      >
        {task === "reviews" ? (
          <>
            <div className="hub-actions">
              <button type="button" disabled={busy} onClick={() => void run(() => listHubReviews(project), setReviews)}>
                Review history
              </button>
            </div>
            {reviews && (
              <div className="hub-collab-block">
                <h4>Review history (head {reviews.head ?? 0})</h4>
                {reviews.state !== "completed" && <p role="status">{reviews.reason}</p>}
                <ul>
                  {(reviews.events ?? []).map((event) => (
                    <li key={`${event.command_id}-${event.sequence}`}>
                      <strong>{event.kind}</strong> by {event.actor}@{event.issuer} · evidence {event.evidence.slice(0, 12)}…
                      {event.release ? ` · release ${event.release.slice(0, 12)}…` : ""} — {event.text}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {searchForm("history")}
            <div className="hub-collab-form" role="group" aria-labelledby="hub-review-actions">
              <h4 id="hub-review-actions">Review actions</h4>
              <label>
                Decision type
                <select value={reviewKind} onChange={(e) => setReviewKind(e.target.value)} disabled={busy}>
                  <option value="comment">comment</option>
                  <option value="assignment">assignment</option>
                  <option value="review-request">review-request</option>
                  <option value="approval">approval</option>
                </select>
              </label>
              <label>
                Evidence SHA-256
                <input value={evidence} onChange={(e) => setEvidence(e.target.value)} disabled={busy} />
              </label>
              {usesRelease ? (
                <label>
                  Release SHA-256
                  <input value={release} onChange={(e) => setRelease(e.target.value)} disabled={busy} />
                </label>
              ) : null}
              {usesRecipient ? (
                <label>
                  Recipient subject ID
                  <input value={recipient} onChange={(e) => setRecipient(e.target.value)} disabled={busy} />
                </label>
              ) : null}
              {usesParent ? (
                <label>
                  Parent command ID
                  <input value={parent} onChange={(e) => setParent(e.target.value)} disabled={busy} aria-describedby="hub-review-parent-help" />
                </label>
              ) : null}
              {usesParent ? (
                <span className="hub-team-note" id="hub-review-parent-help">
                  {reviewKind === "approval" ? "The command ID of the exact review request this approves." : "Optional: the command ID of the exact decision this replies to."}
                </span>
              ) : null}
              <label>
                {textLabel[reviewKind] ?? "Comment"}
                <textarea value={reviewText} onChange={(e) => setTexts({ ...texts, [reviewKind]: e.target.value })} disabled={busy} />
              </label>
              <CommandDetails intent={reviewIntent} />
              <button
                type="button"
                disabled={busy || !evidence}
                onClick={() => {
                  const command = {
                    project,
                    expected: reviews?.head ?? 0,
                    kind: reviewKind,
                    evidence,
                    parent: usesParent ? parent : "",
                    recipient: usesRecipient ? recipient : "",
                    text: reviewText,
                    release: usesRelease ? release : "",
                  };
                  const id = reviewIntent.idFor(command);
                  void run(
                    async () => {
                      const posted = await postHubReview({ ...command, id });
                      // The hub answers a recorded decision with that one event;
                      // the history shown is read again whole, so it never lists
                      // less than the hub holds. A decision already recorded is
                      // never shown as refused because that read failed.
                      if (posted.state !== "completed") return posted;
                      reviewIntent.completed(id);
                      const current = await listHubReviews(project);
                      return current.state === "completed" ? current : posted;
                    },
                    (res) => {
                      setReviews(res);
                      if (res.state !== "completed") setMessage(res.reason ?? "review refused");
                    },
                  );
                }}
              >
                {reviewActions[reviewKind] ?? "Post comment"}
              </button>
            </div>
          </>
        ) : null}

        {task === "notifications" ? (
          <>
            <div className="hub-actions">
              <button type="button" disabled={busy} onClick={() => void run(() => listHubNotifications(project), setNotifications)}>
                Load notifications
              </button>
            </div>
            {notifications && (
              <div className="hub-collab-block">
                <h4>Notifications</h4>
                {notifications.state !== "completed" ? (
                  <p role="status">{notifications.reason ?? "The notifications could not be read."}</p>
                ) : (notifications.events ?? []).length === 0 ? (
                  <p>Nothing in this project is addressed to you.</p>
                ) : null}
                <ul>
                  {(notifications.events ?? []).map((event) => (
                    <li key={`n-${event.command_id}-${event.sequence}`}>
                      {event.kind} · {event.text} (from {event.actor})
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {searchForm("notifications")}
          </>
        ) : null}

        {task === "revisions" ? (
          <>
            <div className="hub-actions">
              <button type="button" disabled={busy} onClick={readLifecycle}>
                Version history
              </button>
            </div>
            {lifecycle && (
              <div className="hub-collab-block">
                <h4>Revision history</h4>
                <p className="hub-team-note">Event head: {eventHead(lifecycle) ?? "not reported"}</p>
                {lifecycle.state !== "completed" && <p role="status">{lifecycle.reason}</p>}
                {lifecycle.warning ? <p className="hub-custody-warning">{lifecycle.warning}</p> : null}
                <ul>
                  {(lifecycle.events ?? []).map((event) => (
                    <li key={`l-${event.command_id}-${event.sequence}`}>
                      #{event.sequence} <strong>{event.kind}</strong> by {event.actor}@{event.issuer}
                      {event.resource ? ` · ${event.resource}` : ""} — {event.reason}
                    </li>
                  ))}
                </ul>
                <h5>Current revision tips</h5>
                <ul>
                  {Object.entries(tipsOf).map(([name, tipIds]) => (
                    <li key={name}>
                      {name}: {tipIds.join(", ") || "(none)"}
                      {tipIds.length > 1
                        ? " — compare, keep-both as a new revision, or resolve with every tip. Do not overwrite silently."
                        : ""}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {refreshProblem ? (
              <p role="status" className="hub-message">
                The recorded action stands; the revision history could not be read again: {refreshProblem}
              </p>
            ) : null}
            <div className="hub-collab-form" role="group" aria-labelledby="hub-revisions">
              <h4 id="hub-revisions">Revisions</h4>
              <label>
                Action
                <select value={revisionKind} onChange={(e) => setRevisionKind(e.target.value as "revision" | "resolve")} disabled={busy}>
                  <option value="revision">revision</option>
                  <option value="resolve">resolve</option>
                </select>
              </label>
              <label>
                Resource ID
                <input value={resource} onChange={(e) => setResource(e.target.value)} disabled={busy} />
              </label>
              <label>
                Artifact SHA-256
                <input value={revisionArtifact} onChange={(e) => setRevisionArtifact(e.target.value)} disabled={busy} />
              </label>
              <label>
                Parent revision IDs
                <input value={parents} onChange={(e) => setParents(e.target.value)} disabled={busy} aria-describedby="hub-revision-parents-help" />
              </label>
              <span className="hub-team-note" id="hub-revision-parents-help">
                {revisionKind === "resolve"
                  ? "Comma-separated: every current tip of this resource."
                  : "Comma-separated: the one tip this revision follows, or empty for the first."}
              </span>
              <label>
                Reason
                <input value={revisionReason} onChange={(e) => setRevisionReason(e.target.value)} disabled={busy} />
              </label>
              <CommandDetails intent={revisionIntent} />
              <button
                type="button"
                disabled={busy || !resource.trim() || !revisionReason.trim()}
                onClick={() =>
                  postLifecycle(
                    {
                      kind: revisionKind,
                      resource,
                      artifact: revisionArtifact,
                      parents: parents
                        .split(",")
                        .map((p) => p.trim())
                        .filter(Boolean),
                      subject: "",
                      until: "",
                      reason: revisionReason,
                    },
                    revisionIntent,
                    setRevisionWrite,
                  )
                }
              >
                {revisionKind === "resolve" ? "Resolve conflict" : "Record revision"}
              </button>
              <LifecycleWrite result={revisionWrite} />
            </div>
            <OfflineRevisionDraft
              workspace={workspace}
              context={{ project, resource, tips: tipsOf[resource] ?? [], head: lifecycle?.head ?? 0 }}
            />
          </>
        ) : null}

        {task === "support" ? (
          <div className="hub-collab-form" role="group" aria-labelledby="hub-support-approvals">
            <h4 id="hub-support-approvals">Support approvals</h4>
            <p>
              Announce the project&rsquo;s sharing policy by its exact bytes, ask a reviewer to approve a
              published value-free summary, and approve the request naming those same bytes — under the
              signed-in identity. The hub&rsquo;s export route serves the summary only under that chain. The
              privacy panel&rsquo;s local approval inputs are separate deliberate acts and are never filled
              from here.
            </p>
            <label>
              Sharing policy file
              <select value={policyEntry} onChange={(e) => setPolicyEntry(e.target.value)} disabled={busy || !workspace}>
                <option value="">(select)</option>
                {sharingPolicies.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Support bundle
              <select value={bundleEntry} onChange={(e) => setBundleEntry(e.target.value)} disabled={busy || !workspace}>
                <option value="">(select)</option>
                {supportBundles.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Reviewer subject ID
              <input value={supportRecipient} onChange={(e) => setSupportRecipient(e.target.value)} disabled={busy} />
            </label>
            <CommandDetails intent={supportIntent} />
            <div className="hub-actions">
              <button type="button" disabled={busy || !workspace || !policyEntry} onClick={() => void supportAct("support-policy")}>
                Announce policy
              </button>
              <button
                type="button"
                disabled={busy || !workspace || !bundleEntry || !supportRecipient.trim()}
                onClick={() => void supportAct("support-request")}
              >
                Request approval
              </button>
              <button type="button" disabled={busy || !workspace || !bundleEntry} onClick={() => void supportAct("support-approval")}>
                Approve summary
              </button>
            </div>
            {supportReview && supportReview.state !== "completed" ? <p role="alert">{supportReview.reason}</p> : null}
            {supportReview?.events?.length ? (
              <p role="status">
                Recorded: {supportReview.events[0]?.kind} by {supportReview.events[0]?.actor}@
                {supportReview.events[0]?.issuer}
                {supportReview.replay ? " (replayed, same command id)" : ""}
              </p>
            ) : null}
            <p className="hub-team-note">
              Approval status:{" "}
              {supportKind === "support-approval"
                ? "approved in this window."
                : supportKind === "support-request"
                  ? "requested; not yet approved."
                  : "no request or approval recorded in this window."}
            </p>
            <label htmlFor="hub-export-digest">Summary SHA-256</label>
            <input
              id="hub-export-digest"
              value={exportDigest}
              onChange={(e) => setExportDigest(e.target.value.trim())}
              disabled={busy}
              placeholder="Filled by the request or approval above; the hub refuses any digest its approval chain does not name"
            />
            <label htmlFor="hub-export-destination">Summary download file</label>
            <input
              id="hub-export-destination"
              value={exportDestination}
              onChange={(e) => setExportDestination(e.target.value)}
              disabled={busy}
            />
            <button
              type="button"
              disabled={busy || !exportDigest || !exportDestination.trim()}
              onClick={() =>
                void run(() => downloadHubExport({ project, digest: exportDigest, destination_path: exportDestination.trim() }), setExportResult)
              }
            >
              Download summary
            </button>
            {exportResult ? (
              <p role="status" className={exportResult.state === "completed" ? undefined : "hub-message"}>
                Export {exportResult.transfer_state || exportResult.state}
                {exportResult.path ? ` — ${exportResult.path}` : ""}
                {exportResult.reason ? ` — ${exportResult.reason}` : ""}
                {exportResult.warning ? ` — ${exportResult.warning}` : ""}
              </p>
            ) : null}
          </div>
        ) : null}

        {task === "administration" ? (
          <>
            <p className="hub-team-note">
              {capabilities.includes("admin")
                ? "The service grants you admin on this project."
                : "The service has not granted you admin on this project; it will refuse these actions and say why."}{" "}
              The hub decides every action; this window only asks.
            </p>
            <div className="hub-collab-form" role="group" aria-labelledby="hub-access">
              <h4 id="hub-access">Access</h4>
              <label>
                User subject ID
                <input
                  value={removeSubject}
                  onChange={(e) => {
                    setRemoveSubject(e.target.value);
                    setConfirmingRemoval(false);
                  }}
                  disabled={busy}
                />
              </label>
              <label>
                Reason
                <input value={removeReason} onChange={(e) => setRemoveReason(e.target.value)} disabled={busy} />
              </label>
              <CommandDetails intent={accessIntent} />
              {confirmingRemoval ? (
                <div role="group" aria-label="Confirm user removal">
                  <p role="alert">
                    Remove {removeSubject} from {project}? The hub refuses their new requests; copies they already
                    downloaded stay with them and cannot be revoked.
                  </p>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      setConfirmingRemoval(false);
                      postLifecycle(
                        { kind: "remove-user", resource: "", artifact: "", parents: [], subject: removeSubject, until: "", reason: removeReason },
                        accessIntent,
                        setAccessWrite,
                      );
                    }}
                  >
                    Confirm removal
                  </button>
                  <button type="button" disabled={busy} onClick={() => setConfirmingRemoval(false)}>
                    Cancel
                  </button>
                </div>
              ) : (
                <button type="button" disabled={busy || !removeSubject.trim() || !removeReason.trim()} onClick={() => setConfirmingRemoval(true)}>
                  Remove user
                </button>
              )}
              <LifecycleWrite result={accessWrite} />
            </div>

            <div className="hub-collab-form" role="group" aria-labelledby="hub-retention">
              <h4 id="hub-retention">Retention</h4>
              <label>
                Action
                <select
                  value={retentionKind}
                  onChange={(e) => {
                    setRetentionKind(e.target.value as "retention" | "retire");
                    setConfirmingRetire(false);
                  }}
                  disabled={busy}
                >
                  <option value="retention">retention</option>
                  <option value="retire">retire</option>
                </select>
              </label>
              <label>
                Artifact SHA-256
                <input value={retentionArtifact} onChange={(e) => setRetentionArtifact(e.target.value)} disabled={busy} />
              </label>
              {retentionKind === "retention" ? (
                <>
                  <label>
                    Retain until
                    <input value={until} onChange={(e) => setUntil(e.target.value)} disabled={busy} aria-describedby="hub-retain-until-help" />
                  </label>
                  <span className="hub-team-note" id="hub-retain-until-help">
                    A full RFC 3339 timestamp with its offset, such as 2027-01-31T00:00:00Z; never read as local time.
                  </span>
                </>
              ) : null}
              <label>
                Reason
                <input value={retentionReason} onChange={(e) => setRetentionReason(e.target.value)} disabled={busy} />
              </label>
              <CommandDetails intent={retentionIntent} />
              {retentionKind === "retire" && confirmingRetire ? (
                <div role="group" aria-label="Confirm retirement">
                  <p role="alert">Retire artifact {retentionArtifact}? Copies already downloaded stay under local custody.</p>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      setConfirmingRetire(false);
                      postLifecycle(
                        { kind: "retire", resource: "", artifact: retentionArtifact, parents: [], subject: "", until: "", reason: retentionReason },
                        retentionIntent,
                        setRetentionWrite,
                      );
                    }}
                  >
                    Confirm retirement
                  </button>
                  <button type="button" disabled={busy} onClick={() => setConfirmingRetire(false)}>
                    Cancel
                  </button>
                </div>
              ) : (
                <button
                  type="button"
                  disabled={busy || !retentionArtifact.trim() || !retentionReason.trim() || (retentionKind === "retention" && !until.trim())}
                  onClick={() =>
                    retentionKind === "retire"
                      ? setConfirmingRetire(true)
                      : postLifecycle(
                          { kind: "retention", resource: "", artifact: retentionArtifact, parents: [], subject: "", until, reason: retentionReason },
                          retentionIntent,
                          setRetentionWrite,
                        )
                  }
                >
                  {retentionKind === "retire" ? "Retire artifact" : "Set retention"}
                </button>
              )}
              <LifecycleWrite result={retentionWrite} />
            </div>

            <div className="hub-collab-form" role="group" aria-labelledby="hub-audit-export">
              <h4 id="hub-audit-export">Audit export</h4>
              <p className="hub-team-note">An explicit read of the project&rsquo;s recorded decisions. Nothing is saved until you choose to.</p>
              <label>
                Reason
                <input value={auditReason} onChange={(e) => setAuditReason(e.target.value)} disabled={busy} />
              </label>
              <CommandDetails intent={auditIntent} />
              <button
                type="button"
                disabled={busy || !auditReason.trim()}
                onClick={() => {
                  setAuditSaved(null);
                  postLifecycle(
                    { kind: "audit-export", resource: "", artifact: "", parents: [], subject: "", until: "", reason: auditReason },
                    auditIntent,
                    setAudit,
                  );
                }}
              >
                Submit audit export
              </button>
              {audit && audit.state !== "completed" ? <p role="alert">{audit.reason}</p> : null}
              {audit?.audit ? (
                <div className="hub-collab-block">
                  <h5>Audit export</h5>
                  <p className="hub-custody-warning">{audit.audit.warning}</p>
                  <h6>Review events</h6>
                  {audit.audit.reviews.length === 0 ? <p>No review events.</p> : null}
                  <ul>
                    {audit.audit.reviews.map((event) => (
                      <li key={`ar-${event.command_id}-${event.sequence}`}>
                        #{event.sequence} {event.kind} by {event.actor}@{event.issuer} — {event.text}
                      </li>
                    ))}
                  </ul>
                  <h6>Lifecycle events</h6>
                  {audit.audit.lifecycle.length === 0 ? <p>No lifecycle events.</p> : null}
                  <ul>
                    {audit.audit.lifecycle.map((event) => (
                      <li key={`al-${event.command_id}-${event.sequence}`}>
                        #{event.sequence} {event.kind} by {event.actor}@{event.issuer} — {event.reason}
                      </li>
                    ))}
                  </ul>
                  <button type="button" disabled={busy} onClick={() => void run(() => saveHubAudit(project), (saved) => { if (saved.state !== "cancelled") setAuditSaved(saved); })}>
                    Save audit file…
                  </button>
                  {auditSaved ? (
                    <p role="status" className={auditSaved.state === "completed" ? undefined : "hub-message"}>
                      {auditSaved.state === "completed" ? `Saved the audit export to ${auditSaved.path}. ${auditSaved.warning ?? ""}` : auditSaved.reason}
                    </p>
                  ) : null}
                </div>
              ) : null}
            </div>
          </>
        ) : null}
      </TaskTabs>

      {capabilities.length > 0 && (
        <p className="hub-team-note">Effective capabilities from the service: {capabilities.join(", ")}</p>
      )}
      {message && (
        <p role="status" className="hub-message">
          {message}
        </p>
      )}
    </section>
  );
}

/** A lifecycle write as the hub answered it: the recorded event's kind,
 * actor, subject or resource and event number, or its refusal. */
function LifecycleWrite({ result }: { result: HubLifecycleResult | null }) {
  if (!result) return null;
  if (result.state !== "completed") return <p role="alert">{result.reason}</p>;
  if (!result.event) return null;
  return (
    <p role="status">
      {recordedLine(result.event)}
      {result.replay ? " (replayed, same command id)" : ""}
    </p>
  );
}

/** Local retention of an edited file as an offline revision branch. It stays
 * available after sign-out for the project and resource it was last shown
 * with; reconnecting uploads nothing, and no grant, token or approval is kept
 * in the draft. */
export function OfflineRevisionDraft({ workspace, context }: { workspace: string; context: RevisionContext }) {
  const [localPath, setLocalPath] = useState("");
  const [drafts, setDrafts] = useState<EditorDraftsResult | null>(null);
  const { running, run } = useLifecycle<"working">();
  const busy = running !== null;
  return (
    <div className="hub-collab-form" role="group" aria-labelledby="hub-offline-draft">
      <h4 id="hub-offline-draft">Offline revision draft</h4>
      <p>
        Retain a local edit of {context.resource || "a resource"} in {context.project} as an offline revision branch.
        It is local work only: reconnecting uploads nothing, and a revision is posted only by your explicit action
        against fresh service state. Approvals are never stored in drafts.
      </p>
      <label>
        Edited file
        <input value={localPath} onChange={(e) => setLocalPath(e.target.value)} disabled={busy} />
      </label>
      <button
        type="button"
        disabled={busy || !localPath || !workspace || !context.resource}
        onClick={() =>
          void run("working", async () => {
            setDrafts(
              await saveHubOfflineDraft({
                workspace,
                project: context.project,
                resource: context.resource,
                parent_tips: context.tips,
                local_path: localPath,
                expected_head: context.head,
                note: "Retained offline hub revision branch",
              }),
            );
          })
        }
      >
        Save offline draft
      </button>
      {drafts && (
        <p role="status">
          Drafts retained: {(drafts.drafts ?? []).filter((d) => d.kind === "hub-revision").length} ({drafts.state}
          {drafts.reason ? ` — ${drafts.reason}` : ""})
        </p>
      )}
    </div>
  );
}
