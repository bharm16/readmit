import { useState } from "react";
import {
  listHubReviews,
  postHubReview,
  postHubSupportReview,
  listHubNotifications,
  listHubLifecycle,
  postHubLifecycle,
  downloadHubExport,
  explainHubCustody,
  saveHubOfflineDraft,
  type HubReviewsResult,
  type HubLifecycleResult,
  type HubResult,
  type HubTransferResult,
  type EditorDraftsResult,
  type Artifact,
} from "./bindings";

type Props = {
  project: string;
  workspace?: string;
  entries?: Artifact[];
  capabilities?: string[] | undefined;
};

export function TeamCollaboration({ project, workspace = "", entries = [], capabilities = [] }: Props) {
  const [reviews, setReviews] = useState<HubReviewsResult | null>(null);
  const [notifications, setNotifications] = useState<HubReviewsResult | null>(null);
  const [lifecycle, setLifecycle] = useState<HubLifecycleResult | null>(null);
  const [custody, setCustody] = useState<HubResult | null>(null);
  const [drafts, setDrafts] = useState<EditorDraftsResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const [commentText, setCommentText] = useState("Evidence-linked comment");
  const [evidence, setEvidence] = useState("");
  const [recipient, setRecipient] = useState("");
  const [reviewKind, setReviewKind] = useState("comment");
  const [release, setRelease] = useState("");
  const [parent, setParent] = useState("");
  const [commandId, setCommandId] = useState("review-cmd-1");

  const [resource, setResource] = useState("case-one");
  const [artifact, setArtifact] = useState("");
  const [parents, setParents] = useState("");
  const [subject, setSubject] = useState("");
  const [until, setUntil] = useState("");
  const [lifeKind, setLifeKind] = useState("revision");
  const [lifeId, setLifeId] = useState("life-cmd-1");
  const [lifeReason, setLifeReason] = useState("Explicit lifecycle action");
  const [localPath, setLocalPath] = useState("");

  // The sharing journey: the policy and bundle entries of the open workspace,
  // the reviewer asked to approve, and the summary digest an approval named —
  // the digest the hub's export route serves under that chain. The privacy
  // panel's local approval inputs are separate deliberate acts and are never
  // filled from here.
  const sharingPolicies = entries.filter((entry) => entry.kind === "sharing-policy").map((entry) => entry.name);
  const supportBundles = entries.filter((entry) => entry.kind === "support").map((entry) => entry.name);
  const [policyEntry, setPolicyEntry] = useState("");
  const [bundleEntry, setBundleEntry] = useState("");
  const [supportRecipient, setSupportRecipient] = useState("");
  const [supportId, setSupportId] = useState("");
  const [supportReview, setSupportReview] = useState<HubReviewsResult | null>(null);
  const [exportDigest, setExportDigest] = useState("");
  const [exportDestination, setExportDestination] = useState("");
  const [exportResult, setExportResult] = useState<HubTransferResult | null>(null);

  function supportEntriesFor(kind: "support-policy" | "support-request" | "support-approval") {
    return {
      project,
      workspace,
      entry: kind === "support-policy" ? policyEntry : bundleEntry,
      kind,
      id: supportId.trim(),
      recipient: kind === "support-request" ? supportRecipient.trim() : "",
    };
  }

  function supportAct(kind: "support-policy" | "support-request" | "support-approval") {
    return run(() => postHubSupportReview(supportEntriesFor(kind)), (res) => {
      setSupportReview(res);
      if (res.state === "completed" && kind !== "support-policy") {
        setExportDigest(res.events?.[0]?.evidence ?? "");
      }
    });
  }

  async function run<T>(work: () => Promise<T>, apply: (value: T) => void) {
    setBusy(true);
    setMessage(null);
    try {
      apply(await work());
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="hub-team-section" aria-label="Team collaboration">
      <h3>Team reviews, conflicts and administration</h3>
      <p className="hub-team-note">
        Decisions use the authenticated hub identity. A local reviewer name cannot approve.
        Stale heads and changed grants require a renewed action. Membership and IdP assignment
        stay under customer-admin access policy — this panel does not edit raw policy JSON.
      </p>

      <div className="hub-actions">
        <button type="button" disabled={busy} onClick={() => void run(() => listHubReviews(project), setReviews)}>
          Load review history
        </button>
        <button type="button" disabled={busy} onClick={() => void run(() => listHubNotifications(project), setNotifications)}>
          Load notifications
        </button>
        <button type="button" disabled={busy} onClick={() => void run(() => listHubLifecycle(project), setLifecycle)}>
          Load lifecycle and tips
        </button>
        <button type="button" disabled={busy} onClick={() => void run(() => explainHubCustody(), setCustody)}>
          Explain downloaded-copy limits
        </button>
      </div>

      {custody && (
        <aside className="hub-custody-warning" role="note">
          <strong>Custody:</strong> {custody.custody_warning}
          {custody.reason ? <p>{custody.reason}</p> : null}
        </aside>
      )}

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

      {notifications && (
        <div className="hub-collab-block">
          <h4>Notifications</h4>
          <ul>
            {(notifications.events ?? []).map((event) => (
              <li key={`n-${event.command_id}-${event.sequence}`}>
                {event.kind} · {event.text} (from {event.actor})
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="hub-collab-form">
        <h4>Post collaboration decision</h4>
        <label>
          Kind
          <select value={reviewKind} onChange={(e) => setReviewKind(e.target.value)} disabled={busy}>
            <option value="comment">comment</option>
            <option value="assignment">assignment</option>
            <option value="review-request">review-request</option>
            <option value="approval">approval</option>
          </select>
        </label>
        <label>
          Command id
          <input value={commandId} onChange={(e) => setCommandId(e.target.value)} disabled={busy} />
        </label>
        <label>
          Evidence digest
          <input value={evidence} onChange={(e) => setEvidence(e.target.value)} disabled={busy} />
        </label>
        <label>
          Recipient subject
          <input value={recipient} onChange={(e) => setRecipient(e.target.value)} disabled={busy} />
        </label>
        <label>
          Parent command id
          <input value={parent} onChange={(e) => setParent(e.target.value)} disabled={busy} />
        </label>
        <label>
          Release digest
          <input value={release} onChange={(e) => setRelease(e.target.value)} disabled={busy} />
        </label>
        <label>
          Text
          <textarea value={commentText} onChange={(e) => setCommentText(e.target.value)} disabled={busy} />
        </label>
        <button
          type="button"
          disabled={busy || !evidence}
          onClick={() =>
            void run(
              async () => {
                const posted = await postHubReview({
                  project,
                  id: commandId,
                  expected: reviews?.head ?? 0,
                  kind: reviewKind,
                  evidence,
                  parent,
                  recipient,
                  text: commentText,
                  release,
                });
                // The hub answers a recorded decision with that one event;
                // the history shown is read again whole, so it never lists
                // less than the hub holds. A decision already recorded is
                // never shown as refused because that read failed.
                if (posted.state !== "completed") return posted;
                const current = await listHubReviews(project);
                return current.state === "completed" ? current : posted;
              },
              (res) => {
                setReviews(res);
                if (res.state !== "completed") setMessage(res.reason ?? "review refused");
              },
            )
          }
        >
          Submit review decision
        </button>
      </div>

      <div className="hub-collab-form">
        <h4>Team sharing approvals (support)</h4>
        <p>
          Announce the project&rsquo;s sharing policy by its exact bytes, ask a reviewer to approve a
          published value-free summary, and approve the request naming those same bytes — under the
          signed-in identity. The hub&rsquo;s export route serves the summary only under that chain. The
          privacy panel&rsquo;s local approval inputs are separate deliberate acts and are never filled
          from here.
        </p>
        <label>
          Sharing policy entry
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
          Published support bundle
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
          Support command id
          <input value={supportId} onChange={(e) => setSupportId(e.target.value)} disabled={busy} />
        </label>
        <label>
          Reviewer to ask (request)
          <input value={supportRecipient} onChange={(e) => setSupportRecipient(e.target.value)} disabled={busy} />
        </label>
        <div className="hub-actions">
          <button
            type="button"
            disabled={busy || !workspace || !policyEntry || !supportId.trim()}
            onClick={() => void supportAct("support-policy")}
          >
            Announce sharing policy
          </button>
          <button
            type="button"
            disabled={busy || !workspace || !bundleEntry || !supportId.trim() || !supportRecipient.trim()}
            onClick={() => void supportAct("support-request")}
          >
            Request support approval
          </button>
          <button
            type="button"
            disabled={busy || !workspace || !bundleEntry || !supportId.trim()}
            onClick={() => void supportAct("support-approval")}
          >
            Approve this summary
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
        <label htmlFor="hub-export-digest">Approved summary digest</label>
        <input
          id="hub-export-digest"
          value={exportDigest}
          onChange={(e) => setExportDigest(e.target.value.trim())}
          disabled={busy}
          placeholder="Filled by the request or approval above; the hub refuses any digest its approval chain does not name"
        />
        <label htmlFor="hub-export-destination">Download the approved summary to</label>
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
          Download approved support summary
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

      {lifecycle && (
        <div className="hub-collab-block">
          <h4>Lifecycle (head {lifecycle.head ?? 0})</h4>
          {lifecycle.state !== "completed" && <p role="status">{lifecycle.reason}</p>}
          <p className="hub-custody-warning">{lifecycle.warning}</p>
          <h5>Unresolved tips</h5>
          <ul>
            {Object.entries(lifecycle.tips ?? {}).map(([name, tipIds]) => (
              <li key={name}>
                {name}: {tipIds.join(", ") || "(none)"} — compare, keep-both as a new revision, or resolve with every tip. Do not overwrite silently.
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="hub-collab-form">
        <h4>Lifecycle / conflict / admin</h4>
        <label>
          Kind
          <select value={lifeKind} onChange={(e) => setLifeKind(e.target.value)} disabled={busy}>
            <option value="revision">revision</option>
            <option value="resolve">resolve</option>
            <option value="remove-user">remove-user</option>
            <option value="retention">retention</option>
            <option value="retire">retire</option>
            <option value="audit-export">audit-export</option>
          </select>
        </label>
        <label>
          Command id
          <input value={lifeId} onChange={(e) => setLifeId(e.target.value)} disabled={busy} />
        </label>
        <label>
          Resource
          <input value={resource} onChange={(e) => setResource(e.target.value)} disabled={busy} />
        </label>
        <label>
          Artifact digest
          <input value={artifact} onChange={(e) => setArtifact(e.target.value)} disabled={busy} />
        </label>
        <label>
          Parents (comma-separated tip ids)
          <input value={parents} onChange={(e) => setParents(e.target.value)} disabled={busy} />
        </label>
        <label>
          Subject (remove-user)
          <input value={subject} onChange={(e) => setSubject(e.target.value)} disabled={busy} />
        </label>
        <label>
          Until (retention RFC3339)
          <input value={until} onChange={(e) => setUntil(e.target.value)} disabled={busy} />
        </label>
        <label>
          Reason
          <input value={lifeReason} onChange={(e) => setLifeReason(e.target.value)} disabled={busy} />
        </label>
        <button
          type="button"
          disabled={busy}
          onClick={() =>
            void run(
              () =>
                postHubLifecycle({
                  project,
                  id: lifeId,
                  expected: lifecycle?.head ?? 0,
                  kind: lifeKind,
                  resource: lifeKind === "revision" || lifeKind === "resolve" ? resource : "",
                  artifact: ["revision", "resolve", "retention", "retire"].includes(lifeKind) ? artifact : "",
                  parents: parents
                    .split(",")
                    .map((p) => p.trim())
                    .filter(Boolean),
                  subject: lifeKind === "remove-user" ? subject : "",
                  until: lifeKind === "retention" ? until : "",
                  reason: lifeReason,
                }),
              (res) => {
                setLifecycle(res);
                if (res.state !== "completed") setMessage(res.reason ?? "lifecycle refused");
              },
            )
          }
        >
          Submit lifecycle command
        </button>
      </div>

      <div className="hub-collab-form">
        <h4>Offline draft branch</h4>
        <p>
          Retain a local offline edit branch, then reconnect and post an explicit revision with the
          expected head. Retries reuse the same command id; approvals are never stored in drafts.
        </p>
        <label>
          Local edited path
          <input value={localPath} onChange={(e) => setLocalPath(e.target.value)} disabled={busy} />
        </label>
        <button
          type="button"
          disabled={busy || !localPath || !workspace}
          onClick={() =>
            void run(
              () =>
                saveHubOfflineDraft({
                  workspace,
                  project,
                  resource,
                  parent_tips: lifecycle?.tips?.[resource] ?? [],
                  local_path: localPath,
                  expected_head: lifecycle?.head ?? 0,
                  note: "Retained offline hub revision branch",
                }),
              setDrafts,
            )
          }
        >
          Retain offline draft
        </button>
        {drafts && (
          <p role="status">
            Drafts retained: {(drafts.drafts ?? []).filter((d) => d.kind === "hub-revision").length} ({drafts.state}
            {drafts.reason ? ` — ${drafts.reason}` : ""})
          </p>
        )}
      </div>

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
