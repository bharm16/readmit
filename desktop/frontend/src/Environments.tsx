// Environments: the project's named test systems. The list opens a read-only
// environment; every change is one Edit sheet and one Save of one revision.
// Nothing here connects on opening: Test connection, Check destination, Check
// reference and Reset each happen only when pressed.
import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import {
  chooseEnvironmentFile,
  checkCredential,
  checkEnvironment,
  checkEnvironmentDestination,
  listWholeCatalog,
  listCredentials,
  locateItem,
  newIntentId,
  openItemDraft,
  recordCredentialRotation,
  removeCredential,
  removeItem,
  saveCredential,
  saveItem,
  type CaptureObservationBinding,
  type CatalogItem,
  type CredentialRow,
  type EnvironmentCheckResult,
  type EnvironmentReport,
  type EnvironmentSummary,
  type ItemDraft,
  type ItemRef,
  type Referrer,
  type RequestContext,
  type ResetOperator,
  type SaveItemResult,
  type SecretPurpose,
  type SecretStore,
  type SendPolicyDecision,
  type Target,
  type TargetClassification,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { BackLink, EmptyState, FormDialog, Menu, Modal, ValueRows, type MenuItem, type SubmitFailure } from "./layout";
import { IconButton } from "./IconButton";
import { RESET_OPERATORS, RESET_REASONS, SEND_POLICY_REASONS, TARGET_CLASSIFICATIONS } from "./display";
import { listDate } from "./Projects";
import { NewObservationEditor, useObservation } from "./Observations";
import { ReviewSheet } from "./ReviewSheet";
import { useVocabulary } from "./vocabulary";
import "./environments.css";

/** Where Environments is: its list, one environment, its credentials, or one observation. */
export type EnvironmentPlace =
  | { kind: "list"; adding?: boolean; addingObservation?: boolean }
  | { kind: "environment"; id: string; editing?: boolean }
  | { kind: "credentials"; id: string }
  | { kind: "observation"; id: string; environment?: string; editing?: boolean };

export function environmentPlace(objectId: string | undefined, view: string | undefined): EnvironmentPlace {
  if (!objectId) return view === "add" ? { kind: "list", adding: true } : view === "add-observation" ? { kind: "list", addingObservation: true } : { kind: "list" };
  if (objectId.startsWith("observation:")) {
    const [, id = "", environment] = objectId.split(":");
    return { kind: "observation", id, ...(environment ? { environment } : {}), ...(view === "edit" ? { editing: true } : {}) };
  }
  if (view === "credentials") return { kind: "credentials", id: objectId };
  return view === "edit" ? { kind: "environment", id: objectId, editing: true } : { kind: "environment", id: objectId };
}

export const TRANSPORTS: { value: "tls" | "plain"; label: string }[] = [
  { value: "tls", label: "TLS" },
  { value: "plain", label: "TCP/MLLP" },
];

const PURPOSES: Record<SecretPurpose, string> = { "mllp-endpoint": "MLLP endpoint", "source-endpoint": "Evidence source" };
const STORES: Record<SecretStore, string> = { "os-keychain": "OS keychain", "customer-managed": "Customer-managed vault" };
const ROTATIONS: Record<string, string> = { current: "Current", overdue: "Overdue", "not-declared": "Not declared" };

/** How a connection check's outcome reads. */
export const CHECK_OUTCOMES: Record<string, string> = {
  reachable: "Reachable",
  unsolicited_bytes: "Reachable, sent unexpected bytes",
  connection_refused: "Connection refused",
  timeout: "Timed out",
  cancelled: "Stopped",
  disconnected: "Disconnected",
  network_error: "Network error",
  unusable: "Not checked",
  tls_certificate_expired: "Certificate expired",
  tls_untrusted_authority: "Untrusted certificate authority",
  tls_hostname_mismatch: "Server name does not match",
  tls_client_certificate_rejected: "Client certificate rejected",
  tls_handshake_failed: "TLS handshake failed",
};

const RESET_OUTCOMES: Record<string, string> = {
  confirmed: "Done",
  unconfirmed: "Not confirmed",
  failed: "Failed",
  refused: "Refused",
  cancelled: "Stopped",
  not_attempted: "Not run",
};


const RESET_TYPES: Record<ResetOperator, string> = {
  operator_confirms: "Manual confirmation",
  collection_empty: "Check empty observation",
  endpoint_quiet: "Check endpoint",
  observation_empty: "Check receiver snapshot",
};

/** The types a new reset action offers; a saved receiver-snapshot check keeps
 * its own type when edited. */
const NEW_RESET_TYPES: ResetOperator[] = ["operator_confirms", "collection_empty", "endpoint_quiet"];

/** A whole number typed into a numeric field, or null when it is not one. An
 * empty field is 0, which the facade reads as its default. */
export function wholeNumber(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === "") return 0;
  return /^\d+$/.test(trimmed) ? Number(trimmed) : null;
}

function summaryOf(item: CatalogItem | null | undefined): EnvironmentSummary | undefined {
  return item?.summary.environment;
}

function classificationText(code: string | undefined): string {
  return TARGET_CLASSIFICATIONS[(code || "unclassified") as TargetClassification] ?? "Not classified";
}

export function fileName(path: string | undefined): string {
  if (!path) return "";
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

/** A refused save, said at the field it names when the sheet has it. */
export function saveProblem(answer: SaveItemResult, fields: Record<string, string>): SubmitFailure {
  if (answer.outcome === "conflict") return { reason: "This changed since you opened it. Close and open it again." };
  const problem = answer.problems.find((entry) => fields[entry.field]) ?? answer.problems[0];
  const field = problem ? fields[problem.field] : undefined;
  return {
    reason: answer.problems.map((entry) => entry.problem).join(" ") || answer.reason || "Not saved.",
    ...(field ? { field } : {}),
  };
}

function referrers(list: Referrer[]): string {
  return list.map((entry) => entry.name).join(", ");
}

/** Everything Environments needs from the window. */
export type EnvironmentsProps = {
  root: string | null;
  context: () => RequestContext;
  place: EnvironmentPlace;
  go: (objectId: string, view?: string) => void;
  back: () => void;
  busy: boolean;
  /** Where an add or edit started from elsewhere (Settings → Security)
   * returns: with the saved connection's ref once saved, without one when
   * closed unsaved. Without it the saved environment opens. */
  onAdded?: ((saved?: string) => void) | undefined;
  /** The retained capture a new observation starts from, when a capture
   * started it. */
  capture?: CaptureObservationBinding | null | undefined;
  /** Where Add observation started from elsewhere returns when it is closed
   * unsaved. */
  onObservationClosed?: (() => void) | undefined;
  /** Where Add observation started from elsewhere returns once saved;
   * without it the saved observation opens. */
  onObservationSaved?: ((id: string) => void) | undefined;
};

/** Environments supplies its page's title, way back, actions and body. */
export function useEnvironments({ root, context, place, go, back, busy, onAdded, capture, onObservationClosed, onObservationSaved }: EnvironmentsProps) {
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [listFailure, setListFailure] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const addRequested = place.kind === "list" && place.adding === true;
  const observationRequested = place.kind === "list" && place.addingObservation === true;
  useEffect(() => {
    if (addRequested) setAdding(true);
  }, [addRequested]);

  const refresh = useCallback(async () => {
    if (!root) return;
    const answer = await listWholeCatalog({ context: context(), kind: "environment", filter: {} });
    if (answer.state === "completed" || answer.state === "empty") {
      setItems(answer.page?.items ?? []);
      setListFailure(null);
    } else {
      setListFailure(answer.reason ?? "The environments could not be read.");
    }
  }, [context, root]);

  useEffect(() => {
    setItems(null);
    void refresh();
  }, [refresh]);

  const envId = place.kind === "environment" || place.kind === "credentials" ? place.id : null;
  const selected = envId ? (items?.find((item) => item.ref.id === envId) ?? null) : null;
  const detail = useEnvironmentDetail({ item: selected, context, refresh, go, back, busy, place, onEdited: onAdded });
  const observation = useObservation({
    id: place.kind === "observation" ? place.id : null,
    environment: place.kind === "observation" ? (items?.find((item) => item.ref.id === place.environment) ?? null) : null,
    context,
    busy,
    onRemoved: back,
    editing: place.kind === "observation" && place.editing === true,
    onEdited: onAdded,
  });

  if (!root) return { title: "Environments", back: null, actions: null, body: null };

  if (place.kind === "observation") return { ...observation, back: <BackLink label={observation.backLabel} onBack={back} /> };
  if (place.kind === "environment" || place.kind === "credentials") {
    return { ...detail, back: <BackLink label={place.kind === "credentials" ? (selected?.name ?? "Environment") : "Environments"} onBack={back} /> };
  }

  const columns: Column<CatalogItem>[] = [
    {
      key: "name",
      header: "Environment",
      priority: 1,
      minWidth: 13.75,
      flex: true,
      render: (item) => (
        <span className="case-name">
          <span title={item.name}>{item.name}</span>
          {item.availability === "available" ? null : <span className="row-reason">{item.reason ?? "Cannot be read"}</span>}
          {item.availability === "missing" ? (
            <button type="button" disabled={busy} onClick={(event) => { event.stopPropagation(); void locateItem({ context: context(), ref: item.ref }).then(refresh); }}>
              Locate
            </button>
          ) : item.availability !== "available" ? (
            <button type="button" disabled={busy} onClick={(event) => { event.stopPropagation(); void refresh(); }}>
              Retry
            </button>
          ) : null}
        </span>
      ),
    },
    { key: "address", header: "Address", priority: 2, minWidth: 13.75, flex: true, render: (item) => summaryOf(item)?.address || "—" },
    { key: "classification", header: "Classification", priority: 3, minWidth: 9, render: (item) => classificationText(summaryOf(item)?.classification) },
    {
      key: "checked",
      header: "Last checked",
      priority: 4,
      minWidth: 9,
      render: (item) => {
        const summary = summaryOf(item);
        if (!summary?.last_checked_at) return "—";
        const outcome = summary.last_check_outcome;
        return outcome && outcome !== "reachable" ? `${listDate(summary.last_checked_at)} · ${CHECK_OUTCOMES[outcome] ?? "Failed"}` : listDate(summary.last_checked_at);
      },
    },
  ];

  const sorted = [...(items ?? [])].sort((a, b) => a.name.localeCompare(b.name) || a.ref.id.localeCompare(b.ref.id));
  let body: ReactNode;
  if (listFailure) {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{listFailure}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (items && items.length === 0) {
    body = (
      <EmptyState
        title="No environments"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => setAdding(true)}>
            Add environment
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Environments"
        className="page-table values-table"
        rows={sorted}
        rowId={(item) => item.ref.id}
        rowLabel={(item) => item.name}
        columns={columns}
        selected={null}
        onSelect={(id) => go(id)}
        onOpen={(id) => go(id)}
        loading={items === null}
      />
    );
  }

  return {
    title: "Environments",
    back: null,
    actions:
      items && items.length > 0 ? (
        <button type="button" className="primary" disabled={busy} onClick={() => setAdding(true)}>
          Add environment
        </button>
      ) : null,
    body: (
      <>
        {body}
        <ConnectionSheet
          open={adding}
          mode="add"
          context={context}
          item={null}
          onClose={() => {
            setAdding(false);
            if (addRequested && onAdded) onAdded();
          }}
          onSaved={async (saved) => {
            setAdding(false);
            await refresh();
            if (addRequested && onAdded) onAdded(`environment:${saved.id}`);
            else go(saved.id);
          }}
        />
        <NewObservationEditor
          open={observationRequested}
          context={context}
          capture={capture}
          onClose={() => (onObservationClosed ? onObservationClosed() : back())}
          onSaved={(saved) => (onObservationSaved ? onObservationSaved(saved.id) : go(`observation:${saved.id}`))}
        />
      </>
    ),
  };
}

// ---------- One environment ----------

function useEnvironmentDetail({
  item,
  context,
  refresh,
  go,
  back,
  busy,
  place,
  onEdited,
}: {
  item: CatalogItem | null;
  context: () => RequestContext;
  refresh: () => Promise<void>;
  go: (objectId: string, view?: string) => void;
  back: () => void;
  busy: boolean;
  place: EnvironmentPlace;
  /** Where an edit started from elsewhere returns once it is saved or closed. */
  onEdited?: ((saved?: string) => void) | undefined;
}) {
  const [draft, setDraft] = useState<ItemDraft | null>(null);
  const [draftFailure, setDraftFailure] = useState<string | null>(null);
  const [sheet, setSheet] = useState<
    null | "connection" | "approve" | "check" | "ranges" | "destinations" | "check-destination" | "reset-edit" | "reset" | "remove" | "details" | "observation" | "observation-link"
  >(null);
  // The project's named observations, which the Observation group and the
  // reset's observation checks name.
  const [observations, setObservations] = useState<CatalogItem[]>([]);
  const [check, setCheck] = useState<EnvironmentCheckResult | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const ref = item?.ref ?? null;

  const reload = useCallback(async () => {
    if (!ref) return;
    const [answer, named] = await Promise.all([openItemDraft({ context: context(), ref }), listWholeCatalog({ context: context(), kind: "observation", filter: {} })]);
    setObservations(named.page?.items ?? []);
    if (answer.state === "completed" && answer.draft) {
      setDraft(answer.draft);
      setDraftFailure(null);
    } else {
      setDraftFailure(answer.reason ?? "This environment could not be read.");
    }
  }, [context, ref?.id, ref?.revision]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setCheck(null);
    setDraft(null);
    void reload();
  }, [reload]);

  const saved = async () => {
    setSheet(null);
    await refresh();
  };

  // Opened to edit from elsewhere (Settings → Security): the connection sheet
  // opens as soon as the draft is read.
  const editingFromElsewhere = place.kind === "environment" && place.editing === true;
  useEffect(() => {
    if (editingFromElsewhere && draft) setSheet("connection");
  }, [editingFromElsewhere, draft !== null]); // eslint-disable-line react-hooks/exhaustive-deps

  /** Saves the environment with one change applied to its whole draft. */
  const saveWith = async (change: (draft: ItemDraft) => ItemDraft, fields: Record<string, string>): Promise<SubmitFailure | null> => {
    if (!ref || !draft) return { reason: "This environment is not open." };
    const answer = await saveItem({
      context: context(),
      kind: "environment",
      item: ref.id,
      ...(ref.revision ? { base_revision: ref.revision } : {}),
      draft: change(draft),
      intent_id: newIntentId(),
    });
    if (answer.outcome !== "saved") return saveProblem(answer, fields);
    await saved();
    return null;
  };

  if (!item || !ref) {
    return { title: "Environment", actions: null, body: <p aria-live="polite">Reading…</p> };
  }
  if (place.kind === "credentials") {
    return credentialsPage({ item, context, busy });
  }

  const summary = summaryOf(item);
  const target = draft?.environment;
  const tls = target?.transport === "tls";
  const observationRef = summary?.observation ?? null;
  const reset = draft?.reset;
  const names = draft?.links?.action_names ?? [];
  const address = target?.address || summary?.address || "";
  // A saved transport to anything but this computer is approved on its own,
  // by a reviewed action, before it is checked or sent to.
  const unapproved = summary?.approval_required === true && !summary.transport_approved && Boolean(target?.transport);
  const observationName = (id: string | undefined) => observations.find((entry) => entry.ref.id === id)?.name ?? "Observation no longer in the project";

  const checkedLine = (): ReactNode => {
    if (check) {
      if (check.state !== "completed" || !check.report) return <span role="alert">{check.reason ?? "The connection was not checked."}</span>;
      return <CheckOutcome report={check.report} at={check.checked_at} />;
    }
    if (summary?.last_checked_at) {
      const outcome = summary.last_check_outcome ?? "";
      const older = summary.last_check_revision && summary.last_check_revision !== ref.revision;
      return `${CHECK_OUTCOMES[outcome] ?? "Checked"} · ${checkedAt(summary.last_checked_at)}${older ? " · before the last edit" : ""}`;
    }
    return "Not checked";
  };

  const effectOf = (action: NonNullable<ItemDraft["reset"]>["actions"][number]): string => {
    switch (action.operator) {
      case "operator_confirms":
        return action.instructions || "—";
      case "collection_empty":
        return observationName(action.observation);
      case "observation_empty":
        return "Receiver snapshot";
      default:
        return address || "—";
    }
  };

  const body = (
    <div className="object-page">
      {item.availability !== "available" ? (
        <p role="alert" className="object-problem">
          {item.reason ?? "This environment cannot be read."}
        </p>
      ) : null}
      {draftFailure || notice ? (
        <p role="alert" className="object-problem">
          {notice ?? draftFailure}
        </p>
      ) : null}
      <section className="value-group" aria-labelledby="environment-connection">
        <header className="value-group-header">
          <h2 id="environment-connection">Connection</h2>
          <button type="button" disabled={busy || !draft} onClick={() => setSheet("connection")}>
            Edit
          </button>
        </header>
        <ValueRows
          rows={[
            { label: "Address", value: address || "—" },
            { label: "Transport", value: `${tls ? "TLS" : target?.transport === "plain" ? "TCP/MLLP" : "—"}${unapproved ? " · Not approved" : ""}` },
            { label: "Classification", value: classificationText(target?.classification ?? summary?.classification) },
            ...(tls && target?.server_name ? [{ label: "Server name", value: target.server_name }] : []),
            ...(tls && target?.ca_file ? [{ label: "CA certificate", value: fileName(target.ca_file) }] : []),
            ...(tls && target?.client_certificate ? [{ label: "Client certificate", value: fileName(target.client_certificate) }] : []),
            ...(target?.credential?.reference ? [{ label: "Credential", value: target.credential.reference }] : []),
            { label: "Last checked", value: checkedLine() },
          ]}
        />
      </section>

      <section className="value-group" aria-labelledby="environment-observation">
        <header className="value-group-header">
          <h2 id="environment-observation">Observation</h2>
          {observationRef ? (
            <button type="button" disabled={busy || !draft} onClick={() => setSheet("observation-link")}>
              Edit
            </button>
          ) : null}
        </header>
        {observationRef ? (
          <button type="button" className="object-link" onClick={() => go(`observation:${observationRef.id}:${ref.id}`)}>
            <span title={summary?.observation_name}>{summary?.observation_name || "Observation"}</span>
            <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
              <path d="M6 3l5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        ) : (
          <div className="value-empty">
            <span>No observation</span>
            {observations.length > 0 ? (
              <button type="button" disabled={busy || !draft} onClick={() => setSheet("observation-link")}>
                Choose observation
              </button>
            ) : null}
            <button type="button" disabled={busy || !draft} onClick={() => setSheet("observation")}>
              Add observation
            </button>
          </div>
        )}
      </section>

      <section className="value-group" aria-labelledby="environment-reset">
        <header className="value-group-header">
          <h2 id="environment-reset">Reset</h2>
          {reset && reset.actions.length > 0 ? (
            <>
              <button type="button" disabled={busy || !draft} onClick={() => setSheet("reset-edit")}>
                Edit
              </button>
              <button type="button" disabled={busy} onClick={() => setSheet("reset")}>
                Reset
              </button>
            </>
          ) : null}
        </header>
        {reset && reset.actions.length > 0 ? (
          <>
            {draft?.links?.reset_name ? <ValueRows rows={[{ label: "Name", value: draft.links.reset_name }]} /> : null}
            <table className="plain-table" aria-label="Reset actions">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Type</th>
                  <th scope="col">Effect</th>
                </tr>
              </thead>
              <tbody>
                {reset.actions.map((action, index) => (
                  <tr key={action.id || index}>
                    <th scope="row">{names[index] || RESET_TYPES[action.operator]}</th>
                    <td>{RESET_TYPES[action.operator] ?? RESET_OPERATORS[action.operator]}</td>
                    <td>{effectOf(action)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        ) : (
          <div className="value-empty">
            <span>No reset</span>
            <button type="button" disabled={busy || !draft} onClick={() => setSheet("reset-edit")}>
              Add reset
            </button>
          </div>
        )}
      </section>

      <ConnectionSheet
        open={sheet === "connection"}
        mode="edit"
        context={context}
        item={item}
        draft={draft}
        onClose={() => {
          setSheet(null);
          if (editingFromElsewhere && onEdited) onEdited();
        }}
        onSaved={async () => {
          await saved();
          if (editingFromElsewhere && onEdited) onEdited(`environment:${ref.id}`);
        }}
      />
      <ReviewSheet
        open={sheet === "approve"}
        title="Approve transport"
        action="environment.approve-transport"
        finalLabel="Approve"
        context={context}
        items={[ref]}
        onClose={() => setSheet(null)}
        onDone={() => void refresh()}
        consequence={`Readmit may then connect to ${address} this way; nothing is sent now.`}
        render={(review) => (
          <ValueRows
            rows={[
              { label: "Environment", value: item.name },
              { label: "Address", value: review.transport?.address ?? address },
              { label: "Transport", value: review.transport?.transport === "tls" ? "TLS" : "TCP/MLLP" },
              ...(review.transport?.server_name ? [{ label: "Server name", value: review.transport.server_name }] : []),
              ...(review.transport?.ca_file ? [{ label: "CA certificate", value: fileName(review.transport.ca_file) }] : []),
              ...(review.transport?.client_certificate ? [{ label: "Client certificate", value: fileName(review.transport.client_certificate) }] : []),
              { label: "Classification", value: classificationText(review.transport?.classification) },
            ]}
          />
        )}
        outcome={() => <p role="status">Transport approved</p>}
      />
      <FormDialog
        open={sheet === "check"}
        title="Test connection"
        size="small"
        submitLabel="Test connection"
        onClose={() => setSheet(null)}
        onSubmit={async () => {
          const answer = await checkEnvironment({ context: context(), ref });
          setCheck(answer);
          setSheet(null);
          if (answer.state === "completed") void refresh();
          return null;
        }}
      >
        <p className="consequence">Connects to {address}; no messages are sent.</p>
      </FormDialog>
      <Modal
        open={sheet === "ranges"}
        title="Allowed destinations"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={() => setSheet("destinations")}>
              Edit
            </button>
            <button type="button" className="primary" onClick={() => setSheet(null)}>
              Done
            </button>
          </div>
        }
      >
        {(draft?.policy?.approved_destinations ?? []).length > 0 ? (
          <table className="plain-table" aria-label="Allowed ranges">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Range</th>
              </tr>
            </thead>
            <tbody>
              {(draft?.policy?.approved_destinations ?? []).map((range, index) => (
                <tr key={`${range}-${index}`}>
                  <th scope="row">{draft?.links?.range_names?.[index] || "—"}</th>
                  <td>{range}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p>No allowed ranges</p>
        )}
      </Modal>
      <DestinationsSheet
        open={sheet === "destinations"}
        ranges={draft?.policy?.approved_destinations ?? []}
        names={draft?.links?.range_names ?? []}
        onClose={() => setSheet(null)}
        onSave={(ranges, rangeNames) =>
          saveWith(
            (held) => ({
              ...held,
              policy: { schema: held.policy?.schema || "readmit-send-policy/v1", approved_destinations: ranges },
              links: { ...(held.links ?? {}), range_names: rangeNames },
            }),
            {
              "policy.approved_destinations": "destination-range-0",
              "links.range_names": "destination-name-0",
              ...Object.fromEntries(ranges.map((_, index) => [`links.range_names.${index}`, `destination-name-${index}`])),
            },
          )
        }
      />
      <CheckDestinationSheet
        open={sheet === "check-destination"}
        context={context}
        environment={ref}
        classification={(target?.classification || summary?.classification || "unclassified") as TargetClassification}
        onClose={() => setSheet(null)}
      />
      <ResetEditSheet
        open={sheet === "reset-edit"}
        draft={draft}
        address={address}
        observations={observations}
        onClose={() => setSheet(null)}
        onSave={(plan, name, actionNames) =>
          saveWith(
            (held) => ({
              ...held,
              reset: plan,
              links: { ...(held.links ?? {}), reset_name: name, action_names: actionNames },
            }),
            { "reset.actions": "reset-add-action", "links.reset_name": "reset-name" },
          )
        }
      />
      <ReviewSheet
        open={sheet === "reset"}
        title="Reset"
        action="environment.reset"
        finalLabel="Reset"
        blocked={(review, confirmed) =>
          review.requirements.includes("confirmations") &&
          (review.reset?.actions ?? []).some((entry) => entry.type === "operator_confirms" && !confirmed.includes(entry.id))
        }
        context={context}
        items={[ref]}
        onClose={() => setSheet(null)}
        onDone={() => void refresh()}
        consequence="Readmit checks each action and deletes nothing."
        render={(review, confirmed, setConfirmed) => (
          <>
            <ValueRows rows={[{ label: "Environment", value: review.reset?.target || item.name }, ...(review.reset?.name ? [{ label: "Reset", value: review.reset.name }] : [])]} />
            <ol className="review-actions" aria-label="Reset actions">
              {(review.reset?.actions ?? []).map((action) => (
                <li key={action.id}>
                  <p className="review-action-name">
                    <strong>{action.name || RESET_TYPES[action.type]}</strong> · {RESET_TYPES[action.type]}
                  </p>
                  <p>{action.type === "operator_confirms" ? action.instructions : action.effect}</p>
                  {action.type === "operator_confirms" ? (
                    <label className="check">
                      <input
                        type="checkbox"
                        checked={confirmed.includes(action.id)}
                        onChange={(event) => setConfirmed(event.target.checked ? [...confirmed, action.id] : confirmed.filter((id) => id !== action.id))}
                      />
                      Done
                    </label>
                  ) : null}
                </li>
              ))}
            </ol>
          </>
        )}
        outcome={(result) => (
          <ol className="review-actions" aria-label="Reset results">
            {(result.reset?.result?.actions ?? []).map((action, index) => (
              <li key={action.id || index}>
                <strong>{actionName(names, reset, action.id)}</strong> · {RESET_OUTCOMES[action.outcome] ?? "Not run"}
                {action.outcome !== "confirmed" && RESET_REASONS[action.reason] ? <p>{RESET_REASONS[action.reason]}</p> : null}
              </li>
            ))}
          </ol>
        )}
      />
      <RemoveEnvironmentSheet open={sheet === "remove"} context={context} item={item} onClose={() => setSheet(null)} onRemoved={async () => { setSheet(null); await refresh(); back(); }} />
      <Modal open={sheet === "details"} title="Details" onClose={() => setSheet(null)}>
        <ValueRows
          rows={[
            { label: "Name", value: item.name },
            { label: "Created", value: listDate(item.created_at) },
            { label: "Updated", value: listDate(item.updated_at) },
          ]}
        />
      </Modal>
      <ObservationLinkSheet
        open={sheet === "observation-link"}
        chosen={observationRef?.id ?? ""}
        observations={observations}
        onClose={() => setSheet(null)}
        onSave={(id) =>
          saveWith(
            (held) => {
              const links = { ...(held.links ?? {}) };
              if (id) links.observation = id;
              else delete links.observation;
              return { ...held, links };
            },
            { "links.observation": "environment-observation-choice" },
          )
        }
      />
      <NewObservationEditor
        open={sheet === "observation"}
        context={context}
        onClose={() => setSheet(null)}
        onSaved={async (observation) => {
          const failure = await saveWith((held) => ({ ...held, links: { ...(held.links ?? {}), observation: observation.id } }), {});
          if (failure) setNotice(failure.reason);
          else go(`observation:${observation.id}:${ref.id}`);
        }}
      />
    </div>
  );

  const menu: MenuItem[] = [
    { label: "Credentials", onSelect: () => go(ref.id, "credentials") },
    { label: "Allowed destinations", onSelect: () => setSheet("ranges"), disabled: !draft },
    { label: "Check destination…", onSelect: () => setSheet("check-destination") },
    {
      label: "Duplicate",
      disabled: !draft,
      separated: true,
      onSelect: () => {
        if (!draft) return;
        setNotice(null);
        void saveItem({ context: context(), kind: "environment", draft: { ...draft, name: `${item.name} copy` }, intent_id: newIntentId() }).then(async (answer) => {
          if (answer.outcome === "saved" && answer.saved) {
            await refresh();
            go(answer.saved.id);
          } else {
            setNotice(saveProblem(answer, {}).reason);
          }
        });
      },
    },
    { label: "Details", onSelect: () => setSheet("details") },
    { label: "Remove…", onSelect: () => setSheet("remove"), tone: "danger" as const, separated: true },
  ];

  const unusable = busy || !draft || item.availability !== "available";
  return {
    title: item.name,
    // What the palette lists for this environment: its page's actions, each
    // opening the same sheet or review its button does.
    palette: {
      object: item.name,
      items: [
        unapproved
          ? { label: "Approve transport…", onSelect: () => setSheet("approve"), disabled: unusable }
          : { label: "Test connection…", onSelect: () => setSheet("check"), disabled: unusable },
        ...(reset && reset.actions.length > 0 ? [{ label: "Reset…", onSelect: () => setSheet("reset"), disabled: busy }] : []),
        ...menu,
      ],
    },
    actions: (
      <>
        {unapproved ? (
          <button type="button" disabled={unusable} onClick={() => setSheet("approve")}>
            Approve transport
          </button>
        ) : (
          <button type="button" disabled={unusable} onClick={() => setSheet("check")}>
            Test connection
          </button>
        )}
        <Menu label="More environment actions" items={menu} />
      </>
    ),
    body,
  };
}

function actionName(names: string[], reset: ItemDraft["reset"], id: string): string {
  const index = reset?.actions.findIndex((action) => action.id === id) ?? -1;
  const action = index >= 0 ? reset?.actions[index] : undefined;
  return (index >= 0 ? names[index] : "") || (action ? RESET_TYPES[action.operator] : "Action");
}

/** When a check ran: its date and time. */
function checkedAt(at: string): string {
  return `${listDate(at)} ${new Date(at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}`;
}

function CheckOutcome({ report, at }: { report: EnvironmentReport; at: string | undefined }) {
  const parts = [CHECK_OUTCOMES[report.outcome] ?? "Unsupported result"];
  if (report.tls_version) parts.push(report.tls_version);
  if (at) parts.push(checkedAt(at));
  return <span role="status">{parts.join(" · ")}</span>;
}

// ---------- Connection sheet ----------

function splitAddress(address: string): { host: string; port: string } {
  const at = address.lastIndexOf(":");
  if (at < 0) return { host: address, port: "" };
  return { host: address.slice(0, at).replace(/^\[|\]$/g, ""), port: address.slice(at + 1) };
}

function joinAddress(host: string, port: string): string {
  const bare = host.trim();
  return `${bare.includes(":") ? `[${bare}]` : bare}:${port.trim()}`;
}

/** Add environment (Name, Address, Classification, Transport) or Edit its
 * connection (those, the TLS values TLS needs and the connection settings). */
function ConnectionSheet({
  open,
  mode,
  context,
  item,
  draft = null,
  onClose,
  onSaved,
}: {
  open: boolean;
  mode: "add" | "edit";
  context: () => RequestContext;
  item: CatalogItem | null;
  draft?: ItemDraft | null;
  onClose: () => void;
  onSaved: (saved: ItemRef) => void | Promise<void>;
}) {
  const [base, setBase] = useState<ItemDraft | null>(null);
  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [classification, setClassification] = useState<TargetClassification>("unclassified");
  const [transport, setTransport] = useState<"" | "tls" | "plain">("");
  const [serverName, setServerName] = useState("");
  const [caFile, setCaFile] = useState("");
  const [clientCertificate, setClientCertificate] = useState("");
  const [credential, setCredential] = useState("");
  const [more, setMore] = useState(false);
  const [connectTimeout, setConnectTimeout] = useState("");
  const [messageTimeout, setMessageTimeout] = useState("");
  const [maxAck, setMaxAck] = useState("");
  const [credentials, setCredentials] = useState<CredentialRow[]>([]);
  const [chooseFailure, setChooseFailure] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    const fill = (held: ItemDraft, fresh: boolean) => {
      const target: Partial<Target> = held.environment ?? {};
      const address = splitAddress(target.address ?? "");
      setBase(held);
      setName(fresh ? "" : (held.name ?? item?.name ?? ""));
      setHost(address.host);
      setPort(address.port);
      setClassification((target.classification || "unclassified") as TargetClassification);
      setTransport(fresh ? "" : ((target.transport as "tls" | "plain" | undefined) ?? ""));
      setServerName(target.server_name ?? "");
      setCaFile(target.ca_file ?? "");
      setClientCertificate(target.client_certificate ?? "");
      setCredential(target.credential?.reference ?? "");
      setConnectTimeout(target.connect_timeout ?? "");
      setMessageTimeout(target.message_timeout ?? "");
      setMaxAck(target.max_ack_bytes ? String(target.max_ack_bytes) : "");
      setMore(false);
      setDirty(false);
      setChooseFailure(null);
    };
    if (mode === "edit" && draft) {
      fill(draft, false);
    } else {
      void openItemDraft({ context: context(), ref: { kind: "environment", id: "" } }).then((answer) => {
        if (!cancelled && answer.draft) fill(answer.draft, true);
      });
    }
    void listCredentials({ context: context(), ref: item?.ref ?? { kind: "environment", id: "" } }).then((answer) => {
      if (!cancelled) setCredentials(answer.credentials.filter((row) => row.purpose === "mllp-endpoint"));
    });
    return () => {
      cancelled = true;
    };
  }, [open, mode, draft, item, context]);

  const changed = <T,>(set: (value: T) => void) => (value: T) => {
    set(value);
    setDirty(true);
  };

  const choose = async (kind: "ca-certificate" | "client-certificate", set: (path: string) => void) => {
    setChooseFailure(null);
    const answer = await chooseEnvironmentFile(kind);
    if (answer.state === "completed" && answer.paths?.[0]) {
      set(answer.paths[0]);
      setDirty(true);
    } else if (answer.state !== "cancelled") {
      setChooseFailure(answer.reason ?? "The file was not chosen.");
    }
  };

  // A refusal of a field inside More connection settings opens them first.
  const revealMore = (field: string) => {
    setMore(true);
    setTimeout(() => document.getElementById(field)?.focus());
  };
  const tls = transport === "tls";
  const fields = {
    name: "environment-name",
    "environment.address": "environment-host",
    address: "environment-host",
    transport: "environment-transport-tls",
    "environment.transport": "environment-transport-tls",
    "environment.classification": "environment-classification",
    "environment.server_name": "environment-server-name",
    "environment.connect_timeout": "environment-connect-timeout",
    "environment.message_timeout": "environment-message-timeout",
    "environment.max_ack_bytes": "environment-max-ack",
  };

  return (
    <FormDialog
      open={open}
      title={mode === "add" ? "Add environment" : "Edit connection"}
      submitLabel="Save"
      dirty={dirty}
      submitDisabled={!base}
      onClose={onClose}
      onSubmit={async () => {
        if (!base) return { reason: "The environment is still being read." };
        const ackBytes = wholeNumber(maxAck);
        if (ackBytes === null) {
          revealMore("environment-max-ack");
          return { reason: "Enter a whole number of bytes.", field: "environment-max-ack" };
        }
        const target: Target = {
          ...(base.environment as Target),
          address: host.trim() || port.trim() ? joinAddress(host, port) : "",
          classification,
          transport,
          ...(tls ? { server_name: serverName.trim(), ca_file: caFile, client_certificate: clientCertificate } : { server_name: "", ca_file: "", client_certificate: "" }),
          connect_timeout: connectTimeout.trim(),
          message_timeout: messageTimeout.trim(),
          max_ack_bytes: ackBytes,
        };
        if (credential && tls) target.credential = { secrets_file: base.environment?.credential?.secrets_file || "secrets.json", reference: credential };
        else delete target.credential;
        if (!target.server_name) delete target.server_name;
        if (!target.ca_file) delete target.ca_file;
        if (!target.client_certificate) delete target.client_certificate;
        const answer = await saveItem({
          context: context(),
          kind: "environment",
          ...(item ? { item: item.ref.id } : {}),
          ...(item?.ref.revision ? { base_revision: item.ref.revision } : {}),
          draft: { ...base, name: name.trim(), environment: target },
          intent_id: newIntentId(),
        });
        if (answer.outcome !== "saved" || !answer.saved) {
          const failure = saveProblem(answer, fields);
          if (failure.field && MORE_FIELDS.includes(failure.field)) revealMore(failure.field);
          return failure;
        }
        setDirty(false);
        await onSaved(answer.saved);
        return null;
      }}
    >
      <label htmlFor="environment-name">Name</label>
      <input id="environment-name" type="text" autoFocus value={name} onChange={(event) => changed(setName)(event.target.value)} />
      <div className="field-pair">
        <div>
          <label htmlFor="environment-host">Host</label>
          <input id="environment-host" type="text" spellCheck={false} value={host} onChange={(event) => changed(setHost)(event.target.value)} />
        </div>
        <div className="field-narrow">
          <label htmlFor="environment-port">Port</label>
          <input id="environment-port" type="text" inputMode="numeric" value={port} onChange={(event) => changed(setPort)(event.target.value)} />
        </div>
      </div>
      <label htmlFor="environment-classification">Classification</label>
      <select id="environment-classification" value={classification} onChange={(event) => changed(setClassification)(event.target.value as TargetClassification)}>
        {(["unclassified", "nonproduction", "production"] as TargetClassification[]).map((value) => (
          <option key={value} value={value}>
            {TARGET_CLASSIFICATIONS[value]}
          </option>
        ))}
      </select>
      {classification === "production" ? <p className="consequence">Readmit never sends to a production environment.</p> : null}
      <fieldset className="checks">
        <legend>Transport</legend>
        {TRANSPORTS.map((choice) => (
          <label key={choice.value} className="check">
            <input
              id={`environment-transport-${choice.value}`}
              type="radio"
              name="environment-transport"
              checked={transport === choice.value}
              onChange={() => changed(setTransport)(choice.value)}
            />
            {choice.label}
          </label>
        ))}
      </fieldset>
      {mode === "edit" && tls ? (
        <>
          <label htmlFor="environment-server-name">Server name</label>
          <input id="environment-server-name" type="text" spellCheck={false} value={serverName} onChange={(event) => changed(setServerName)(event.target.value)} />
          <FilePicker label="CA certificate" path={caFile} onChoose={() => void choose("ca-certificate", setCaFile)} onClear={() => changed(setCaFile)("")} />
          <FilePicker label="Client certificate" path={clientCertificate} onChoose={() => void choose("client-certificate", setClientCertificate)} onClear={() => changed(setClientCertificate)("")} />
          {chooseFailure ? <p className="field-error" role="alert">{chooseFailure}</p> : null}
        </>
      ) : null}
      {mode === "edit" && tls ? (
        <>
          <label htmlFor="environment-credential">Credential</label>
          <select id="environment-credential" value={credential} onChange={(event) => changed(setCredential)(event.target.value)}>
            <option value="">None</option>
            {credentials.map((row) => (
              <option key={row.name} value={row.name}>
                {row.name}
              </option>
            ))}
            {credential && !credentials.some((row) => row.name === credential) ? <option value={credential}>{credential}</option> : null}
          </select>
        </>
      ) : null}
      {mode === "edit" ? (
        <>
          <div>
            <button type="button" className="quiet" aria-expanded={more} onClick={() => setMore(!more)}>
              More connection settings
            </button>
          </div>
          {more ? (
            <>
              <label htmlFor="environment-connect-timeout">Connection timeout</label>
              <input id="environment-connect-timeout" type="text" value={connectTimeout} onChange={(event) => changed(setConnectTimeout)(event.target.value)} />
              <label htmlFor="environment-message-timeout">Message timeout</label>
              <input id="environment-message-timeout" type="text" value={messageTimeout} onChange={(event) => changed(setMessageTimeout)(event.target.value)} />
              <label htmlFor="environment-max-ack">Maximum ACK size (bytes)</label>
              <input id="environment-max-ack" type="text" inputMode="numeric" value={maxAck} onChange={(event) => changed(setMaxAck)(event.target.value)} />
            </>
          ) : null}
        </>
      ) : null}
    </FormDialog>
  );
}

const MORE_FIELDS = ["environment-connect-timeout", "environment-message-timeout", "environment-max-ack"];

/** A file an editor names: the chosen file's name, Choose, and Clear. */
export function FilePicker({ id: chooseId, label, path, onChoose, onClear }: { id?: string; label: string; path: string; onChoose: () => void; onClear?: () => void }) {
  const id = useId();
  return (
    <div className="file-picker">
      <span className="field-label" id={id}>
        {label}
      </span>
      <div className="value-with-action" aria-labelledby={id}>
        <span className="location-value" title={path}>
          {path ? fileName(path) : "None"}
        </span>
        <span className="row-actions">
          {path && onClear ? (
            <button type="button" className="quiet" aria-label={`Clear ${label.toLowerCase()}`} onClick={onClear}>
              Clear
            </button>
          ) : null}
          <button type="button" {...(chooseId ? { id: chooseId } : {})} aria-label={`Choose ${label.toLowerCase()}`} onClick={onChoose}>
            Choose…
          </button>
        </span>
      </div>
    </div>
  );
}

// ---------- Allowed destinations ----------

type RangeRow = { name: string; range: string };

function DestinationsSheet({
  open,
  ranges,
  names,
  onSave,
  onClose,
}: {
  open: boolean;
  ranges: string[];
  names: string[];
  onSave: (ranges: string[], names: string[]) => Promise<SubmitFailure | null>;
  onClose: () => void;
}) {
  const [rows, setRows] = useState<RangeRow[]>([]);
  // The rows are seeded as the sheet opens, never while it is open: a page
  // render that hands the same saved lists anew must not discard edits.
  const wasOpen = useRef(false);
  useEffect(() => {
    if (open && !wasOpen.current) setRows(ranges.length > 0 ? ranges.map((range, index) => ({ name: names[index] ?? "", range })) : [{ name: "", range: "" }]);
    wasOpen.current = open;
  }, [open, ranges, names]);
  const used = rows.map((row) => ({ name: row.name.trim(), range: row.range.trim() })).filter((row) => row.name !== "" || row.range !== "");
  const saved = ranges.map((range, index) => ({ name: names[index] ?? "", range }));
  const change = (index: number, value: Partial<RangeRow>) => setRows((held) => held.map((row, at) => (at === index ? { ...row, ...value } : row)));
  return (
    <FormDialog
      open={open}
      title="Edit allowed destinations"
      submitLabel="Save"
      dirty={JSON.stringify(used) !== JSON.stringify(saved)}
      onClose={onClose}
      onSubmit={() => {
        const incomplete = used.findIndex((row) => row.name === "" || row.range === "");
        if (incomplete >= 0) {
          const at = rows.findIndex((row) => row.name.trim() === used[incomplete]!.name && row.range.trim() === used[incomplete]!.range);
          return { reason: "Name each range and give its addresses.", field: used[incomplete]!.name === "" ? `destination-name-${at}` : `destination-range-${at}` };
        }
        return onSave(
          used.map((row) => row.range),
          used.map((row) => row.name),
        );
      }}
    >
      <table className="plain-table edit-table" aria-label="Allowed ranges">
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Range</th>
            <th scope="col">
              <span className="visually-hidden">Remove</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={index}>
              <td>
                <input id={`destination-name-${index}`} type="text" aria-label={`Name of range ${index + 1}`} value={row.name} onChange={(event) => change(index, { name: event.target.value })} />
              </td>
              <td>
                <input
                  id={`destination-range-${index}`}
                  type="text"
                  spellCheck={false}
                  aria-label={`Range ${index + 1}`}
                  value={row.range}
                  onChange={(event) => change(index, { range: event.target.value })}
                />
              </td>
              <td>
                <IconButton
                  icon="close"
                  label={`Remove range ${index + 1}`}
                  onClick={() => setRows((held) => (held.length > 1 ? held.filter((_, at) => at !== index) : [{ name: "", range: "" }]))}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div>
        <button type="button" className="quiet" onClick={() => setRows((held) => [...held, { name: "", range: "" }])}>
          Add range
        </button>
      </div>
    </FormDialog>
  );
}

function decisionText(decision: SendPolicyDecision | undefined): string {
  if (!decision) return "";
  const why = SEND_POLICY_REASONS[decision.reason] ?? "Unsupported reason";
  return decision.allowed ? `Allowed · ${why}` : `Refused · ${why}`;
}

function CheckDestinationSheet({
  open,
  context,
  environment,
  classification: saved,
  onClose,
}: {
  open: boolean;
  context: () => RequestContext;
  environment: ItemRef;
  /** The environment's saved classification, which a check starts from. */
  classification: TargetClassification;
  onClose: () => void;
}) {
  const [address, setAddress] = useState("");
  const [classification, setClassification] = useState<TargetClassification>(saved);
  const [answer, setAnswer] = useState<string | null>(null);
  useEffect(() => {
    if (open) {
      setAddress("");
      setClassification(saved);
      setAnswer(null);
    }
  }, [open, saved]);
  return (
    <FormDialog
      open={open}
      title="Check destination"
      size="small"
      submitLabel="Check"
      submitDisabled={address.trim() === ""}
      status={answer ? <p role="status">{answer}</p> : null}
      onClose={onClose}
      onSubmit={async () => {
        const result = await checkEnvironmentDestination({ context: context(), ref: environment, address: address.trim(), classification });
        if (result.state !== "completed" || !result.decision) return { reason: result.reason ?? "The destination was not checked." };
        setAnswer(decisionText(result.decision));
        return null;
      }}
    >
      <label htmlFor="destination-address">Address</label>
      <input id="destination-address" type="text" autoFocus spellCheck={false} value={address} onChange={(event) => { setAddress(event.target.value); setAnswer(null); }} />
      <label htmlFor="destination-classification">Classification</label>
      <select id="destination-classification" value={classification} onChange={(event) => { setClassification(event.target.value as TargetClassification); setAnswer(null); }}>
        {(["unclassified", "nonproduction", "production"] as TargetClassification[]).map((value) => (
          <option key={value} value={value}>
            {TARGET_CLASSIFICATIONS[value]}
          </option>
        ))}
      </select>
      <p className="consequence">Simulates a proposed send against the saved ranges; nothing is sent.</p>
    </FormDialog>
  );
}

// ---------- Observation ----------

/** Which named observation the environment reads its results from, or none. */
function ObservationLinkSheet({
  open,
  chosen,
  observations,
  onSave,
  onClose,
}: {
  open: boolean;
  chosen: string;
  observations: CatalogItem[];
  onSave: (id: string) => Promise<SubmitFailure | null>;
  onClose: () => void;
}) {
  const [value, setValue] = useState(chosen);
  useEffect(() => {
    if (open) setValue(chosen);
  }, [open, chosen]);
  return (
    <FormDialog open={open} title="Observation" size="small" submitLabel="Save" dirty={value !== chosen} onClose={onClose} onSubmit={() => onSave(value)}>
      <label htmlFor="environment-observation-choice">Observation</label>
      <select id="environment-observation-choice" value={value} onChange={(event) => setValue(event.target.value)}>
        <option value="">None</option>
        {observations.map((entry) => (
          <option key={entry.ref.id} value={entry.ref.id}>
            {entry.name}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

// ---------- Reset ----------

type ResetRow = { id: string; name: string; operator: ResetOperator; instructions: string; observation: string };

/** The reset's name and its ordered actions. Each action is added or edited in
 * its own sheet; nothing runs until the reset itself is reviewed. */
function ResetEditSheet({
  open,
  draft,
  address,
  observations,
  onSave,
  onClose,
}: {
  open: boolean;
  draft: ItemDraft | null;
  address: string;
  observations: CatalogItem[];
  onSave: (plan: NonNullable<ItemDraft["reset"]>, name: string, actionNames: string[]) => Promise<SubmitFailure | null>;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [rows, setRows] = useState<ResetRow[]>([]);
  const [dirty, setDirty] = useState(false);
  // The action its own sheet edits: an index, or "new".
  const [editing, setEditing] = useState<number | "new" | null>(null);
  // Each operator's authority is the facade's, published in its vocabulary.
  const vocabulary = useVocabulary();
  const authorityOf = (operator: ResetOperator) => vocabulary?.reset_operators.find((entry) => entry.operator === operator)?.authority ?? "none";
  useEffect(() => {
    if (!open) return;
    const plan = draft?.reset;
    const names = draft?.links?.action_names ?? [];
    setName(draft?.links?.reset_name ?? "");
    setRows((plan?.actions ?? []).map((action, index) => ({ id: action.id, name: names[index] ?? "", operator: action.operator, instructions: action.instructions, observation: action.observation ?? "" })));
    setDirty(false);
    setEditing(null);
  }, [open, draft]);
  const change = (next: ResetRow[]) => {
    setRows(next);
    setDirty(true);
  };
  const effectOf = (row: ResetRow) =>
    row.operator === "operator_confirms"
      ? row.instructions
      : row.operator === "collection_empty"
        ? (observations.find((entry) => entry.ref.id === row.observation)?.name ?? "—")
        : row.operator === "observation_empty"
          ? "Receiver snapshot"
          : address || "—";
  return (
    <>
      <FormDialog
        open={open && editing === null}
        title="Edit reset"
        size="wide"
        submitLabel="Save"
        dirty={dirty}
        onClose={onClose}
        onSubmit={() => {
          if (name.trim() === "") return { reason: "Name the reset.", field: "reset-name" };
          const plan = {
            schema: draft?.reset?.schema || "readmit-reset-plan/v1",
            environment: draft?.reset?.environment || draft?.environment?.name || "",
            actions: rows.map((row) => ({
              id: row.id,
              operator: row.operator,
              authority: authorityOf(row.operator),
              instructions: row.operator === "operator_confirms" ? row.instructions : "",
              ...(row.operator === "collection_empty" || row.operator === "observation_empty" ? { observation: row.observation } : {}),
            })),
          };
          return onSave(
            plan,
            name.trim(),
            rows.map((row) => row.name),
          );
        }}
      >
        <label htmlFor="reset-name">Name</label>
        <input id="reset-name" type="text" value={name} onChange={(event) => { setName(event.target.value); setDirty(true); }} />
        {rows.length > 0 ? (
          <table className="plain-table" aria-label="Reset actions">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Type</th>
                <th scope="col">Effect</th>
                <th scope="col">
                  <span className="visually-hidden">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, index) => (
                <tr key={`${row.id}-${index}`}>
                  <th scope="row" title={row.name}>
                    {row.name}
                  </th>
                  <td>{RESET_TYPES[row.operator]}</td>
                  <td>{effectOf(row)}</td>
                  <td className="row-actions">
                    <button type="button" className="quiet" onClick={() => setEditing(index)}>
                      Edit
                    </button>
                    <Menu
                      label={`More actions for ${row.name}`}
                      items={[
                        { label: "Move up", disabled: index === 0, onSelect: () => change(rows.map((entry, at) => (at === index - 1 ? rows[index]! : at === index ? rows[index - 1]! : entry))) },
                        { label: "Move down", disabled: index === rows.length - 1, onSelect: () => change(rows.map((entry, at) => (at === index + 1 ? rows[index]! : at === index ? rows[index + 1]! : entry))) },
                        { label: "Remove", tone: "danger" as const, separated: true, onSelect: () => change(rows.filter((_, at) => at !== index)) },
                      ]}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
        <div>
          <button id="reset-add-action" type="button" className="quiet" onClick={() => setEditing("new")}>
            Add action
          </button>
        </div>
      </FormDialog>
      <ResetActionSheet
        open={open && editing !== null}
        row={typeof editing === "number" ? (rows[editing] ?? null) : null}
        address={address}
        observations={observations}
        onClose={() => setEditing(null)}
        onDone={(row) => {
          change(typeof editing === "number" ? rows.map((entry, at) => (at === editing ? row : entry)) : [...rows, row]);
          setEditing(null);
        }}
      />
    </>
  );
}

/** One reset action: its name, its type and what that type needs. Done returns
 * it to the reset, which is saved once. */
function ResetActionSheet({
  open,
  row,
  address,
  observations,
  onDone,
  onClose,
}: {
  open: boolean;
  row: ResetRow | null;
  address: string;
  observations: CatalogItem[];
  onDone: (row: ResetRow) => void;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [operator, setOperator] = useState<ResetOperator | "">("");
  const [instructions, setInstructions] = useState("");
  const [observation, setObservation] = useState("");
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    if (!open) return;
    setName(row?.name ?? "");
    setOperator(row?.operator ?? "");
    setInstructions(row?.instructions ?? "");
    setObservation(row?.observation ?? "");
    setDirty(false);
  }, [open, row]);
  const changed = <T,>(set: (value: T) => void) => (value: T) => {
    set(value);
    setDirty(true);
  };
  const types = row?.operator === "observation_empty" ? [...NEW_RESET_TYPES, "observation_empty" as const] : NEW_RESET_TYPES;
  return (
    <FormDialog
      open={open}
      title={row ? "Edit action" : "Add action"}
      submitLabel="Done"
      dirty={dirty}
      onClose={onClose}
      onSubmit={() => {
        if (name.trim() === "") return { reason: "Name the action.", field: "reset-action-name" };
        if (operator === "") return { reason: "Choose what the action does.", field: "reset-action-type" };
        if (operator === "operator_confirms" && instructions.trim() === "") return { reason: "Say what the person does.", field: "reset-action-instructions" };
        if (operator === "collection_empty" && observation === "") return { reason: "Choose the observation to check.", field: "reset-action-observation" };
        onDone({
          id: row?.id ?? "",
          name: name.trim(),
          operator,
          instructions: operator === "operator_confirms" ? instructions.trim() : "",
          observation: operator === "collection_empty" || operator === "observation_empty" ? observation : "",
        });
        return null;
      }}
    >
      <label htmlFor="reset-action-name">Name</label>
      <input id="reset-action-name" type="text" autoFocus value={name} onChange={(event) => changed(setName)(event.target.value)} />
      <label htmlFor="reset-action-type">Type</label>
      <select id="reset-action-type" value={operator} onChange={(event) => changed(setOperator)(event.target.value as ResetOperator)}>
        {operator === "" ? <option value="">Choose a type</option> : null}
        {types.map((value) => (
          <option key={value} value={value}>
            {RESET_TYPES[value]}
          </option>
        ))}
      </select>
      {operator === "operator_confirms" ? (
        <>
          <label htmlFor="reset-action-instructions">Instructions</label>
          <textarea id="reset-action-instructions" rows={3} value={instructions} onChange={(event) => changed(setInstructions)(event.target.value)} />
        </>
      ) : null}
      {operator === "collection_empty" ? (
        <>
          <label htmlFor="reset-action-observation">Observation</label>
          <select id="reset-action-observation" value={observation} onChange={(event) => changed(setObservation)(event.target.value)}>
            {observation === "" ? <option value="">Choose an observation</option> : null}
            {observations.map((entry) => (
              <option key={entry.ref.id} value={entry.ref.id}>
                {entry.name}
              </option>
            ))}
          </select>
        </>
      ) : null}
      {operator === "endpoint_quiet" ? <ValueRows rows={[{ label: "Endpoint", value: address || "—" }]} /> : null}
    </FormDialog>
  );
}

// ---------- Reviewed actions ----------

function RemoveEnvironmentSheet({ open, context, item, onClose, onRemoved }: { open: boolean; context: () => RequestContext; item: CatalogItem; onClose: () => void; onRemoved: () => void | Promise<void> }) {
  return (
    <FormDialog
      open={open}
      title="Remove environment"
      size="small"
      submitLabel="Remove"
      tone="danger"
      onClose={onClose}
      onSubmit={async () => {
        const answer = await removeItem({ context: context(), ref: item.ref });
        if (answer.state !== "completed") {
          return { reason: answer.referring.length > 0 ? `Used by ${referrers(answer.referring)}. Choose another environment there first.` : (answer.reason ?? "Not removed.") };
        }
        await onRemoved();
        return null;
      }}
    >
      <p>{item.name}</p>
      <p className="consequence">Removes it from this project; its history stays readable.</p>
    </FormDialog>
  );
}

// ---------- Credentials ----------

function credentialsPage({ item, context, busy }: { item: CatalogItem; context: () => RequestContext; busy: boolean }) {
  return { title: "Credentials", actions: <CredentialsActions item={item} context={context} busy={busy} />, body: <CredentialsList item={item} context={context} busy={busy} /> };
}

/** Credentials' header actions and list share one read, so each is its own
 * component over a shared store. */
const credentialStore = { listeners: new Set<() => void>() };

function CredentialsActions({ item, context, busy }: { item: CatalogItem; context: () => RequestContext; busy: boolean }) {
  const [editing, setEditing] = useState(false);
  const [scanning, setScanning] = useState(false);
  return (
    <>
      <button type="button" className="primary" disabled={busy} onClick={() => setEditing(true)}>
        New credential
      </button>
      <Menu label="More credential actions" items={[{ label: "Scan configured files…", onSelect: () => setScanning(true) }]} />
      <CredentialSheet open={editing} context={context} row={null} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); credentialStore.listeners.forEach((listener) => listener()); }} />
      <ReviewSheet
        open={scanning}
        title="Scan configured files"
        action="secret.scan"
        finalLabel="Scan"
        consequence="Only these files are scanned."
        context={context}
        items={[item.ref]}
        onClose={() => setScanning(false)}
        onDone={() => undefined}
        render={(review) => (
          <ul className="plain-list" aria-label="Files to scan">
            {(review.scan?.files ?? []).map((file) => (
              <li key={file}>{file}</li>
            ))}
          </ul>
        )}
        outcome={(result) => (
          result.scan ? (
            <>
              <p role="status">
                {result.scan.scan.files_checked} {result.scan.scan.files_checked === 1 ? "file" : "files"} checked
              </p>
              {result.scan.scan.unresolved_locations.length > 0 ? (
                <ul className="plain-list" aria-label="Found in">
                  {result.scan.scan.unresolved_locations.map((location) => (
                    <li key={location}>{location}</li>
                  ))}
                </ul>
              ) : null}
            </>
          ) : null
        )}
      />
    </>
  );
}

function CredentialsList({ item, context, busy }: { item: CatalogItem; context: () => RequestContext; busy: boolean }) {
  const [rows, setRows] = useState<CredentialRow[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [editing, setEditing] = useState<CredentialRow | null>(null);
  const [removing, setRemoving] = useState<CredentialRow | null>(null);
  const [notice, setNotice] = useState<{ name: string; text: string } | null>(null);
  const read = useCallback(async () => {
    const answer = await listCredentials({ context: context(), ref: item.ref });
    if (answer.state === "completed" || answer.state === "empty") {
      setRows(answer.credentials);
      setFailure(null);
    } else setFailure(answer.reason ?? "The credentials could not be read.");
  }, [context, item.ref]);
  useEffect(() => {
    void read();
    credentialStore.listeners.add(read);
    return () => {
      credentialStore.listeners.delete(read);
    };
  }, [read]);

  if (failure) return <p role="alert">{failure}</p>;
  if (rows && rows.length === 0) return <EmptyState title="No credentials" />;
  const columns: Column<CredentialRow>[] = [
    {
      key: "name",
      header: "Name",
      priority: 1,
      minWidth: 12,
      render: (row) => (
        <span className="case-name">
          <span>{row.name}</span>
          {notice?.name === row.name ? <span className="row-reason">{notice.text}</span> : null}
        </span>
      ),
    },
    { key: "purpose", header: "Purpose", priority: 2, minWidth: 9, render: (row) => PURPOSES[row.purpose] },
    { key: "store", header: "Store", priority: 3, minWidth: 10, render: (row) => STORES[row.store] },
    { key: "rotation", header: "Rotation", priority: 4, minWidth: 8, render: (row) => ROTATIONS[row.rotation] ?? "Unsupported" },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (row) => (
        <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
          <Menu
            label={`More actions for ${row.name}`}
            items={[
              { label: "Edit…", onSelect: () => setEditing(row) },
              {
                label: "Check reference",
                disabled: busy,
                onSelect: () =>
                  void checkCredential({ context: context(), name: row.name }).then((answer) =>
                    setNotice({ name: row.name, text: answer.state === "completed" && answer.resolved ? "Resolved" : (answer.reason ?? "Did not resolve") }),
                  ),
              },
              {
                label: "Record rotation",
                disabled: busy,
                onSelect: () =>
                  void recordCredentialRotation({ context: context(), name: row.name }).then((answer) => {
                    if (answer.state === "completed") setRows(answer.credentials);
                    else setNotice({ name: row.name, text: answer.reason ?? "Rotation was not recorded" });
                  }),
              },
              { label: "Remove reference…", tone: "danger" as const, separated: true, onSelect: () => setRemoving(row) },
            ]}
          />
        </span>
      ),
    },
  ];
  return (
    <>
      <DataTable label="Credentials" className="page-table" rows={rows ?? []} rowId={(row) => row.name} rowLabel={(row) => row.name} columns={columns} selected={null} onSelect={() => undefined} onOpen={(name) => setEditing(rows?.find((row) => row.name === name) ?? null)} loading={rows === null} />
      <CredentialSheet open={editing !== null} context={context} row={editing} onClose={() => setEditing(null)} onSaved={(next) => { setEditing(null); setRows(next); }} />
      <FormDialog
        open={removing !== null}
        title="Remove reference"
        size="small"
        submitLabel="Remove"
        tone="danger"
        onClose={() => setRemoving(null)}
        onSubmit={async () => {
          if (!removing) return null;
          const answer = await removeCredential({ context: context(), name: removing.name });
          if (answer.state !== "completed" && answer.state !== "empty") {
            return { reason: answer.referring.length > 0 ? `Used by ${referrers(answer.referring)}.` : (answer.reason ?? "Not removed.") };
          }
          setRows(answer.credentials);
          setRemoving(null);
          return null;
        }}
      >
        <p>{removing?.name}</p>
        <p className="consequence">Removes the reference only; the secret stays in its store.</p>
      </FormDialog>
    </>
  );
}

function CredentialSheet({
  open,
  context,
  row,
  onClose,
  onSaved,
}: {
  open: boolean;
  context: () => RequestContext;
  row: CredentialRow | null;
  onClose: () => void;
  onSaved: (rows: CredentialRow[]) => void;
}) {
  const [name, setName] = useState("");
  const [purpose, setPurpose] = useState<SecretPurpose>("mllp-endpoint");
  const [store, setStore] = useState<SecretStore>("os-keychain");
  const [address, setAddress] = useState("");
  const [command, setCommand] = useState("");
  const [replace, setReplace] = useState(false);
  const [args, setArgs] = useState<string[]>([""]);
  const [maxAge, setMaxAge] = useState("");
  const [chooseFailure, setChooseFailure] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    if (!open) return;
    setName(row?.name ?? "");
    setPurpose(row?.purpose ?? "mllp-endpoint");
    setStore(row?.store ?? "os-keychain");
    setAddress(row?.address ?? "");
    setCommand(row?.command ?? "");
    setReplace(row === null);
    setArgs([""]);
    setMaxAge(row?.max_age ?? "");
    setChooseFailure(null);
    setDirty(false);
  }, [open, row]);
  const updating = row !== null;
  const changed = <T,>(set: (value: T) => void) => (value: T) => {
    set(value);
    setDirty(true);
  };
  const fields: Record<string, string> = {
    name: "credential-name",
    store: "credential-store",
    address: "credential-address",
    command: "credential-command",
    arguments: "credential-argument-0",
    max_age: "credential-max-age",
  };
  return (
    <FormDialog
      open={open}
      title={updating ? "Edit credential" : "New credential"}
      submitLabel="Save"
      dirty={dirty}
      onClose={onClose}
      onSubmit={async () => {
        const chosenArgs = args.map((arg) => arg.trim()).filter(Boolean);
        const answer = await saveCredential({
          context: context(),
          name: name.trim(),
          update: updating,
          ...(updating ? {} : { purpose }),
          store,
          address: address.trim(),
          command,
          ...(replace ? { arguments: chosenArgs } : {}),
          replace_arguments: replace,
          ...(maxAge.trim() ? { max_age: maxAge.trim() } : {}),
        });
        if (answer.state !== "completed") {
          const problems = answer.problems ?? [];
          const field = problems.map((problem) => fields[problem.field]).find(Boolean);
          return { reason: problems.map((problem) => problem.problem).join(" ") || answer.reason || "Not saved.", ...(field ? { field } : {}) };
        }
        setDirty(false);
        onSaved(answer.credentials);
        return null;
      }}
    >
      <label htmlFor="credential-name">Name</label>
      <input id="credential-name" type="text" autoFocus={!updating} disabled={updating} value={name} onChange={(event) => changed(setName)(event.target.value)} />
      <label htmlFor="credential-purpose">Purpose</label>
      <select id="credential-purpose" disabled={updating} value={purpose} onChange={(event) => changed(setPurpose)(event.target.value as SecretPurpose)}>
        {(Object.keys(PURPOSES) as SecretPurpose[]).map((value) => (
          <option key={value} value={value}>
            {PURPOSES[value]}
          </option>
        ))}
      </select>
      <label htmlFor="credential-store">Store</label>
      <select id="credential-store" value={store} onChange={(event) => changed(setStore)(event.target.value as SecretStore)}>
        {(Object.keys(STORES) as SecretStore[]).map((value) => (
          <option key={value} value={value}>
            {STORES[value]}
          </option>
        ))}
      </select>
      <label htmlFor="credential-address">Allowed address</label>
      <input id="credential-address" type="text" spellCheck={false} value={address} onChange={(event) => changed(setAddress)(event.target.value)} />
      <FilePicker
        id="credential-command"
        label="Locator program"
        path={command}
        onChoose={() =>
          void chooseEnvironmentFile("locator-program").then((answer) => {
            if (answer.state === "completed" && answer.paths?.[0]) changed(setCommand)(answer.paths[0]);
            else if (answer.state !== "cancelled") setChooseFailure(answer.reason ?? "The program was not chosen.");
          })
        }
      />
      {chooseFailure ? <p className="field-error" role="alert">{chooseFailure}</p> : null}
      {updating ? (
        <label className="check">
          <input type="checkbox" checked={replace} onChange={(event) => changed(setReplace)(event.target.checked)} />
          Replace arguments{row && row.argument_count > 0 ? ` (${row.argument_count} stored)` : ""}
        </label>
      ) : null}
      {replace ? (
        <fieldset>
          <legend>Arguments</legend>
          {args.map((arg, index) => (
            <div key={index} className="filter-rule-row">
              <input
                id={`credential-argument-${index}`}
                type="text"
                spellCheck={false}
                aria-label={`Argument ${index + 1}`}
                value={arg}
                onChange={(event) => changed(setArgs)(args.map((value, at) => (at === index ? event.target.value : value)))}
              />
              <IconButton icon="close" label={`Remove argument ${index + 1}`} onClick={() => changed(setArgs)(args.length > 1 ? args.filter((_, at) => at !== index) : [""])} />
            </div>
          ))}
          <button type="button" className="quiet" onClick={() => setArgs((held) => [...held, ""])}>
            Add argument
          </button>
        </fieldset>
      ) : null}
      <label htmlFor="credential-max-age">Maximum age</label>
      <input id="credential-max-age" type="text" value={maxAge} onChange={(event) => changed(setMaxAge)(event.target.value)} />
    </FormDialog>
  );
}
