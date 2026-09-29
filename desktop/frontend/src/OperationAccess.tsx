// Settings › License › Administrator setup: an activation folder an
// administrator supplies or builds, and the account portal's configured
// destination. Each task is its own sheet with its own final action; nothing
// activates on a choice alone, nothing here issues a license, and nothing
// contacts a service. The folder's activation is read on its own: it is not
// this computer's license, and neither refresh stands for the other.
import { useEffect, useRef, useState } from "react";
import {
  activateActivationFolder, activateOperations, chooseLicenseFolder, commercialStatus, createLicenseActivation,
  exportLicenseDocument, operationStatus, releaseOperations, renewLicenseDocument, resolveOperationClock,
  reviewActivationFolder, reviewActivationRenewal, reviewCommercialDestinations, saveCommercialDestinations,
  verifyLicenseDocument,
  type CommercialStatusResult, type LicenseVerifyResult, type OperationResult,
} from "./bindings";
import { licenseDay } from "./ComputerLicense";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, StepDialog, ValueRows, folderName, type FlowStep, type MenuItem, type SubmitFailure } from "./layout";
import { useLifecycle } from "./lifecycle";
import { useViewState } from "./viewstate";

const TERMS: Record<string, string> = {
  active: "Active",
  grace: "Grace period",
  expired: "Expired",
  "not-yet-valid": "Not yet valid",
};

const ENVIRONMENTS: Record<string, string> = { sandbox: "Sandbox", production: "Production" };

/** The selected folder's activation in one word. */
function activationStatus(result: OperationResult): string {
  const clock = result.clock;
  if (!clock) return "Not activated";
  if (clock.released) return "Released";
  if (clock.rollback) return "Clock changed";
  return TERMS[result.term ?? ""] ?? "Unavailable";
}

/** A recorded instant, to the minute, in UTC as it was recorded. */
function recordedTime(instant: string | undefined): string {
  if (!instant) return "—";
  const date = new Date(instant);
  if (Number.isNaN(date.getTime())) return "—";
  return `${date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" })} UTC`;
}

type Sheet = "folder" | "create" | "renew" | "release" | "clock" | "portal" | "details";

/** The Administrator setup page. portalRequest, when it changes, opens the
 * Account portal sheet once, as Security's Edit of the customer portal asks;
 * onPortalConfigured says whether that sheet saved a destination. */
export function AdministratorSetup({
  portalRequest = 0,
  onPortalHandled,
  onPortalConfigured,
}: {
  portalRequest?: number;
  onPortalHandled?: () => void;
  onPortalConfigured?: (saved: boolean) => void;
}) {
  const [operation, setOperation] = useViewState<OperationResult | null>("AdministratorSetup.operation", null);
  const [commercial, setCommercial] = useViewState<CommercialStatusResult | null>("AdministratorSetup.commercial", null);
  const [sheet, setSheet] = useState<Sheet | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const { running, run } = useLifecycle<"working">();
  const busy = running !== null;

  useEffect(() => {
    let live = true;
    void operationStatus().then((value) => { if (live) setOperation(value); });
    void commercialStatus().then((value) => { if (live) setCommercial(value); });
    return () => { live = false; };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // A request is handled once: a remount, or StrictMode's second run, does
  // not open the sheet again.
  const handled = useRef(0);
  useEffect(() => {
    if (portalRequest === 0) handled.current = 0;
    if (portalRequest === 0 || portalRequest === handled.current) return;
    handled.current = portalRequest;
    onPortalHandled?.();
    setSheet("portal");
  }, [portalRequest]); // eslint-disable-line react-hooks/exhaustive-deps

  async function perform<T>(action: () => Promise<T>, apply: (value: T) => void) {
    if (busy) return;
    await run("working", async () => {
      setFailure(null);
      apply(await action());
    });
  }

  const reread = async () => setOperation(await operationStatus());
  const done = async () => {
    setSheet(null);
    await reread();
  };

  const selected = operation?.selected === true && operation.folder !== undefined;
  const clock = operation?.clock;
  const items: MenuItem[] = [
    { label: "Activation folder", onSelect: () => setSheet("folder") },
    { label: "Create activation folder", onSelect: () => setSheet("create") },
    ...(selected ? [{ label: "Renew activation", onSelect: () => setSheet("renew") }] : []),
    ...(selected
      ? [{ label: "Export activation license", onSelect: () => void perform(exportLicenseDocument, (value) => { if (value.state === "failed") setFailure(value.reason ?? "The license was not exported."); }) }]
      : []),
    // Recovery exists only for a rollback that was detected.
    ...(clock?.rollback ? [{ label: "Clock recovery", onSelect: () => setSheet("clock") }] : []),
    ...(selected && clock && !clock.released ? [{ label: "Release activation", tone: "danger" as const, onSelect: () => setSheet("release"), separated: true }] : []),
    ...(selected ? [{ label: "Details", onSelect: () => setSheet("details") }] : []),
  ];
  const portal = commercial?.state === "completed" ? commercial : null;

  return (
    <>
      <section aria-label="Activation folder">
        <div className="section-header">
          <h2>Activation folder</h2>
          <span className="row-actions">
            <IconButton label="Refresh activation" icon="refresh" disabled={busy} onClick={() => void perform(operationStatus, setOperation)} />
            <Menu label="More activation actions" items={items} />
          </span>
        </div>
        {operation && selected ? (
          <ValueRows
            label="Activation folder"
            rows={[
              { label: "Folder", value: <span title={operation.folder}>{folderName(operation.folder ?? "")}</span> },
              { label: "Status", value: activationStatus(operation) },
              ...(clock?.organization ? [{ label: "Organization", value: clock.organization }] : []),
              ...(operation.author ? [{ label: "Licensed user", value: operation.author }] : []),
              ...(operation.device ? [{ label: "Device", value: operation.device }] : []),
              ...(operation.runner_pool ? [{ label: "Runner pool", value: operation.runner_pool }] : []),
              ...(operation.expires ? [{ label: "Expires", value: licenseDay(operation.expires) }] : []),
            ]}
          />
        ) : operation ? (
          <>
            {operation.state === "failed" && operation.reason ? <p role="alert">{operation.reason}</p> : null}
            <EmptyState title="No activation folder" action={<button type="button" onClick={() => setSheet("folder")}>Choose folder</button>} />
          </>
        ) : null}
        {failure ? <p role="alert">{failure}</p> : null}
      </section>

      <section aria-label="Account portal">
        <div className="section-header">
          <h2>Account portal</h2>
          {portal ? (
            <span className="row-actions">
              <button type="button" onClick={() => setSheet("portal")}>Edit</button>
            </span>
          ) : null}
        </div>
        {portal ? (
          <ValueRows
            label="Account portal"
            rows={[
              { label: "Destination", value: <span className="commercial-destination">{portal.portal}</span> },
              { label: "Environment", value: ENVIRONMENTS[portal.environment ?? ""] ?? "—" },
            ]}
          />
        ) : commercial ? (
          <>
            {commercial.state === "failed" ? <p role="alert">{commercial.reason}</p> : null}
            <EmptyState title="No account portal" action={<button type="button" onClick={() => setSheet("portal")}>Set up</button>} />
          </>
        ) : null}
      </section>

      {sheet === "folder" ? <FolderSheet operation={operation} onClose={() => setSheet(null)} onDone={done} /> : null}
      {sheet === "create" ? <CreateSheet onClose={() => setSheet(null)} onDone={done} /> : null}
      {sheet === "renew" && operation ? <RenewSheet operation={operation} onClose={() => setSheet(null)} onDone={done} /> : null}
      {sheet === "release" && operation ? (
        <FormDialog
          open
          title="Release activation?"
          size="small"
          tone="danger"
          submitLabel="Release"
          onClose={() => setSheet(null)}
          onSubmit={() => completedOr(releaseOperations, "The activation was not released.", done)}
        >
          <ValueRows
            rows={[
              { label: "Folder", value: <span title={operation.folder}>{folderName(operation.folder ?? "")}</span> },
              ...(operation.author ? [{ label: "Licensed user", value: operation.author }] : []),
              ...(operation.device ? [{ label: "Device", value: operation.device }] : []),
            ]}
          />
          <p className="consequence">Stops new licensed work admitted through this folder.</p>
        </FormDialog>
      ) : null}
      {sheet === "clock" && operation ? (
        <FormDialog
          open
          title="Clock recovery"
          size="small"
          submitLabel="Resolve"
          onClose={() => setSheet(null)}
          onSubmit={() => completedOr(resolveOperationClock, "The clock change was not resolved.", done)}
        >
          <ValueRows rows={[{ label: "Latest recorded time", value: recordedTime(clock?.high_water) }]} />
          <p className="consequence">Resumes new licensed work admitted through this folder.</p>
        </FormDialog>
      ) : null}
      {sheet === "portal" ? (
        <PortalSheet
          saved={portal}
          onClose={() => {
            setSheet(null);
            onPortalConfigured?.(false);
          }}
          onSaved={(value) => {
            setCommercial(value);
            setSheet(null);
            onPortalConfigured?.(true);
          }}
        />
      ) : null}
      {operation && selected ? (
        <Modal
          open={sheet === "details"}
          title="Activation details"
          size="small"
          onClose={() => setSheet(null)}
          footer={<div className="dialog-footer"><button type="button" onClick={() => setSheet(null)}>Close</button></div>}
        >
          <ValueRows
            rows={[
              { label: "Folder", value: <span className="location-value">{operation.folder}</span> },
              ...(clock ? [{ label: "Issue", value: String(clock.sequence) }, { label: "Latest recorded time", value: recordedTime(clock.high_water) }] : []),
              { label: "Author seats", value: String(operation.author_seats) },
              ...(operation.expires ? [{ label: "Expires", value: licenseDay(operation.expires) }] : []),
              ...(operation.grace_ends ? [{ label: "Grace ends", value: licenseDay(operation.grace_ends) }] : []),
            ]}
          />
        </Modal>
      ) : null}
    </>
  );
}

/** A folder activation change's answer: completed goes on, anything else
 * stays in its sheet with the reason. */
async function completedOr(action: () => Promise<OperationResult>, fallback: string, then: () => Promise<void>): Promise<SubmitFailure | null> {
  const result = await action();
  if (result.state !== "completed") return { reason: result.reason ?? fallback };
  await then();
  return null;
}

/** The folder's selected license and its state. */
function selectedRows(operation: OperationResult) {
  return [
    { label: "Status", value: activationStatus(operation) },
    ...(operation.clock?.organization ? [{ label: "Organization", value: operation.clock.organization }] : []),
    ...(operation.author ? [{ label: "Licensed user", value: operation.author }] : []),
    ...(operation.device ? [{ label: "Device", value: operation.device }] : []),
    ...(operation.expires ? [{ label: "Expires", value: licenseDay(operation.expires) }] : []),
  ];
}

/** Activation folder: the selected folder, or another one chosen and read
 * without selecting it, and Activate. Choosing never activates, and never
 * changes the selection: only a completed activation does. */
function FolderSheet({
  operation,
  onClose,
  onDone,
}: {
  operation: OperationResult | null;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const [reviewed, setReviewed] = useState<OperationResult | null>(null);
  const [choosing, setChoosing] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const selected = operation?.selected === true && operation.folder !== undefined;
  const shown = reviewed ?? (selected ? operation : null);
  return (
    <FormDialog
      open
      title="Activation folder"
      submitLabel="Activate"
      submitDisabled={!shown?.folder || choosing}
      onClose={() => { if (!choosing) onClose(); }}
      status={notice ? <p role="alert">{notice}</p> : null}
      onSubmit={() =>
        completedOr(
          reviewed?.folder ? () => activateActivationFolder({ folder: reviewed.folder ?? "" }) : activateOperations,
          "The activation folder was not activated.",
          onDone,
        )
      }
    >
      <span className="field-label" id="activation-folder-label">Folder</span>
      <div className="value-with-action" aria-labelledby="activation-folder-label">
        <span className="location-value">{shown?.folder ? folderName(shown.folder) : "—"}</span>
        <button
          type="button"
          disabled={choosing}
          onClick={async () => {
            setChoosing(true);
            setNotice(null);
            try {
              const chosen = await reviewActivationFolder();
              if (chosen.folder) setReviewed(chosen);
              else if (chosen.state === "failed") setNotice(chosen.reason ?? null);
            } finally {
              setChoosing(false);
            }
          }}
        >
          {shown?.folder ? "Replace" : "Choose folder"}
        </button>
      </div>
      {shown?.folder ? <ValueRows rows={selectedRows(shown)} /> : null}
    </FormDialog>
  );
}

// Nobody, no device or no runner pool: never a name a license can give.
const NONE = "";

/** Create activation folder: a verified license, the assignments it names,
 * and a new private folder; Create writes the configuration and activates
 * nothing. */
function CreateSheet({ onClose, onDone }: { onClose: () => void; onDone: () => Promise<void> }) {
  const [step, setStep] = useState("license");
  const [verified, setVerified] = useState<LicenseVerifyResult | null>(null);
  const [author, setAuthor] = useState(NONE);
  const [device, setDevice] = useState(NONE);
  const [pool, setPool] = useState(NONE);
  const [folder, setFolder] = useState<string | null>(null);
  const [working, setWorking] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const document = verified?.state === "completed" ? verified.document : undefined;
  const devices = document?.assignments?.find((entry) => entry.author === author)?.devices ?? [];
  const together = (author === NONE) === (device === NONE);

  const pick = async (action: () => Promise<void>) => {
    setWorking(true);
    setNotice(null);
    try {
      await action();
    } finally {
      setWorking(false);
    }
  };

  const steps: FlowStep[] = [
    {
      key: "license",
      label: "License",
      valid: document?.operation_capable === true && !working,
      render: () => (
        <>
          <span className="field-label" id="create-license-label">License</span>
          <div className="value-with-action" aria-labelledby="create-license-label">
            <span className="location-value">{document ? folderName(verified?.entitlement ?? "") : "—"}</span>
            <button
              type="button"
              disabled={working}
              onClick={() => void pick(async () => {
                const result = await verifyLicenseDocument();
                if (result.state === "cancelled") return;
                setVerified(result);
                setAuthor(NONE);
                setDevice(NONE);
                setPool(NONE);
                setFolder(null);
                if (result.state !== "completed") setNotice(result.reason ?? null);
                else if (!result.document?.operation_capable) setNotice("This license cannot create an activation folder.");
              })}
            >
              {document ? "Replace" : "Choose files"}
            </button>
          </div>
          {document ? (
            <ValueRows
              rows={[
                { label: "Plan", value: document.plan },
                { label: "Organization", value: document.organization },
                { label: "Expires", value: licenseDay(document.expires) },
              ]}
            />
          ) : null}
        </>
      ),
    },
    {
      key: "assignment",
      label: "Assignment",
      valid: together,
      render: () => (
        <>
          <label htmlFor="create-user">Licensed user</label>
          <select id="create-user" value={author} onChange={(event) => { setAuthor(event.target.value); setDevice(NONE); }}>
            <option value={NONE}>None</option>
            {(document?.assignments ?? []).map((entry) => <option key={entry.author} value={entry.author}>{entry.author}</option>)}
          </select>
          <label htmlFor="create-device">Device</label>
          <select id="create-device" value={device} disabled={author === NONE} onChange={(event) => setDevice(event.target.value)}>
            <option value={NONE}>None</option>
            {devices.map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
          <label htmlFor="create-pool">Runner pool</label>
          <select id="create-pool" value={pool} onChange={(event) => setPool(event.target.value)}>
            <option value={NONE}>None</option>
            {(document?.authorities ?? []).map((entry) => <option key={entry.id} value={entry.id}>{entry.id}</option>)}
          </select>
          {!together ? <p className="field-error" role="alert">Choose a device for the licensed user.</p> : null}
        </>
      ),
    },
    {
      key: "folder",
      label: "Folder",
      valid: folder !== null && !working,
      render: () => (
        <>
          <span className="field-label" id="create-folder-label">Folder</span>
          <div className="value-with-action" aria-labelledby="create-folder-label">
            <span className="location-value">{folder ?? "—"}</span>
            <button
              type="button"
              disabled={working}
              onClick={() => void pick(async () => {
                const result = await chooseLicenseFolder();
                if (result.state === "completed" && result.folder) setFolder(result.folder);
                else if (result.state === "failed") setNotice(result.reason ?? null);
              })}
            >
              {folder ? "Replace" : "Choose folder"}
            </button>
          </div>
          <ValueRows
            rows={[
              { label: "Plan", value: document?.plan ?? "—" },
              { label: "Licensed user", value: author === NONE ? "None" : author },
              { label: "Device", value: device === NONE ? "None" : device },
              { label: "Runner pool", value: pool === NONE ? "None" : pool },
            ]}
          />
        </>
      ),
    },
  ];

  return (
    <StepDialog
      open
      title="Create activation folder"
      steps={steps}
      step={step}
      onStep={setStep}
      submitLabel="Create"
      submitDisabled={!document || folder === null || !together}
      busy={working}
      onClose={() => { if (!working) onClose(); }}
      status={notice ? <p role="alert">{notice}</p> : null}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        if (!verified || !document || folder === null) return { reason: "Choose the license and the folder first." };
        const result = await createLicenseActivation({
          entitlement: verified.entitlement ?? "",
          trust: verified.trust ?? "",
          author,
          device,
          authority: pool,
          folder,
        });
        if (result.state !== "completed") return { reason: result.reason ?? "The activation folder was not created." };
        await onDone();
        return null;
      }}
    />
  );
}

/** Renew activation: the selected folder's license and a later issue,
 * verified before Install renewal installs exactly those bytes. */
function RenewSheet({ operation, onClose, onDone }: { operation: OperationResult; onClose: () => void; onDone: () => Promise<void> }) {
  const [later, setLater] = useState<LicenseVerifyResult | null>(null);
  const [choosing, setChoosing] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const document = later?.state === "completed" ? later.document : undefined;
  return (
    <FormDialog
      open
      title="Renew activation"
      submitLabel="Install renewal"
      submitDisabled={!document || choosing}
      onClose={() => { if (!choosing) onClose(); }}
      status={notice ? <p role="alert">{notice}</p> : null}
      onSubmit={() =>
        completedOr(
          () => renewLicenseDocument({ entitlement: later?.entitlement ?? "", digest: later?.digest ?? "" }),
          "The renewal was not installed.",
          onDone,
        )
      }
    >
      <ValueRows
        rows={[
          { label: "Folder", value: <span title={operation.folder}>{folderName(operation.folder ?? "")}</span> },
          ...(operation.expires ? [{ label: "Expires", value: licenseDay(operation.expires) }] : []),
        ]}
      />
      <span className="field-label" id="renew-later-label">Later issue</span>
      <div className="value-with-action" aria-labelledby="renew-later-label">
        <span className="location-value">{document ? folderName(later?.entitlement ?? "") : "—"}</span>
        <button
          type="button"
          disabled={choosing}
          onClick={async () => {
            setChoosing(true);
            setNotice(null);
            try {
              const result = await reviewActivationRenewal();
              if (result.state === "cancelled") return;
              setLater(result);
              if (result.state !== "completed") setNotice(result.reason ?? null);
            } finally {
              setChoosing(false);
            }
          }}
        >
          {document ? "Replace" : "Choose file"}
        </button>
      </div>
      {document ? (
        <ValueRows
          rows={[
            { label: "Issue", value: String(document.sequence) },
            { label: "Expires", value: licenseDay(document.expires) },
          ]}
        />
      ) : null}
    </FormDialog>
  );
}

/** Account portal: the operator's configuration file read and shown, and
 * Save keeps it. Opening the portal is the License page's own link. */
function PortalSheet({
  saved,
  onClose,
  onSaved,
}: {
  saved: CommercialStatusResult | null;
  onClose: () => void;
  onSaved: (value: CommercialStatusResult) => void;
}) {
  const [read, setRead] = useState<CommercialStatusResult | null>(null);
  const [choosing, setChoosing] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const shown = read?.state === "completed" ? read : saved;
  return (
    <FormDialog
      open
      title="Account portal"
      submitLabel="Save"
      submitDisabled={read?.state !== "completed" || choosing}
      dirty={read?.state === "completed"}
      onClose={() => { if (!choosing) onClose(); }}
      status={notice ? <p role="alert">{notice}</p> : null}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        const result = await saveCommercialDestinations({ path: read?.config_path ?? "", portal: read?.portal ?? "" });
        if (result.state !== "completed") return { reason: result.reason ?? "The account portal was not saved." };
        onSaved(result);
        return null;
      }}
    >
      <span className="field-label" id="portal-file-label">Configuration</span>
      <div className="value-with-action" aria-labelledby="portal-file-label">
        <span className="location-value">{shown?.config_path ? folderName(shown.config_path) : "—"}</span>
        <button
          type="button"
          disabled={choosing}
          onClick={async () => {
            setChoosing(true);
            setNotice(null);
            try {
              const result = await reviewCommercialDestinations();
              if (result.state === "cancelled") return;
              setRead(result);
              if (result.state !== "completed") setNotice(result.reason ?? null);
            } finally {
              setChoosing(false);
            }
          }}
        >
          {shown ? "Replace" : "Choose file"}
        </button>
      </div>
      {shown?.portal ? (
        <ValueRows
          rows={[
            { label: "Destination", value: <span className="commercial-destination">{shown.portal}</span> },
            { label: "Environment", value: ENVIRONMENTS[shown.environment ?? ""] ?? "—" },
          ]}
        />
      ) : null}
    </FormDialog>
  );
}
