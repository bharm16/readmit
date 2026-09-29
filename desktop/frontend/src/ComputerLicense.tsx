// Settings › License: this computer's license as read-only values, with the
// one action its state calls for, and the rest behind More. Activating and
// renewing are one flow in one sheet — the license (from its file or pasted
// contents, verified here with no network), who and which device it is for,
// and a review of exactly what will be installed — and nothing is installed
// until the final action. The facade decides every fact and every refusal;
// this page only shows them. Administrator setup is its own page.
import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  activateLicense, chooseLicenseFile, commercialStatus, deactivateLicense, exportInstalledLicense, licenseStatus,
  resolveLicenseClock, reviewLicense,
  type InstalledLicenseResult, type InstalledLicenseView, type LicenseDocumentView, type LicenseFileResult,
  type LicenseReviewRequest, type LicenseReviewResult,
} from "./bindings";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, StepDialog, ValueRows, type FlowStep, type MenuItem, type SubmitFailure } from "./layout";
import { useLifecycle } from "./lifecycle";
import { useViewState } from "./viewstate";

/** A signed instant as the calendar day the license states it, in UTC. */
export function licenseDay(instant: string | undefined): string {
  if (!instant) return "—";
  const date = new Date(instant);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric", timeZone: "UTC" });
}

/** What this computer's license lets a person do now: its status word and
 * the one action that state calls for, if any. */
type Primary = "activate" | "renew" | "clock" | null;

export function licenseState(license: InstalledLicenseView): { status: string; primary: Primary } {
  if (license.deactivated) return { status: "Deactivated", primary: "activate" };
  if (license.clock_rollback) return { status: "Clock changed", primary: "clock" };
  if (!license.current_format) return { status: "Legacy format", primary: null };
  switch (license.term) {
    case "not-yet-valid":
      return { status: "Not yet valid", primary: null };
    case "grace":
      return { status: "Grace period", primary: "renew" };
    case "expired":
      return { status: "Expired", primary: "renew" };
    case "active":
      // An activation that was interrupted is finished by activating the
      // same license again.
      if (!license.new_work) return { status: "Activation incomplete", primary: "activate" };
      return { status: "Active", primary: license.renew_soon ? "renew" : null };
    default:
      return { status: "Unavailable", primary: null };
  }
}

type Sheet = "activate" | "renew" | "deactivate" | "clock" | "details";

/** The License page. portal is shown only once an operator configured it;
 * activateRequest, when it changes, opens the flow the license's state calls
 * for, as a task refused for want of a license asks; onActivationEnded says
 * whether that flow installed a license. */
export function ComputerLicense({
  activateRequest = 0,
  onActivateHandled,
  onActivationEnded,
  onAdministratorSetup,
  onOpenRunners,
}: {
  activateRequest?: number;
  /** The request was taken up; the window stops asking. */
  onActivateHandled?: () => void;
  onActivationEnded?: (activated: boolean) => void;
  onAdministratorSetup: () => void;
  onOpenRunners?: () => void;
}) {
  const [status, setStatus] = useViewState<InstalledLicenseResult | null>("License.status", null);
  // Whether this visit's own read has answered, not only a remembered one.
  const [read, setRead] = useState(false);
  const [portal, setPortal] = useState<string | undefined>(undefined);
  const [sheet, setSheet] = useState<Sheet | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const { running, run } = useLifecycle<"working">();
  const busy = running !== null;

  // The status of this computer's license is read on arrival, and the
  // account's address only as the operator configured it: nothing is asked
  // of the account itself.
  useEffect(() => {
    let live = true;
    void licenseStatus().then((value) => {
      if (!live) return;
      setStatus(value);
      setRead(true);
    });
    void commercialStatus().then((value) => { if (live && value.state === "completed" && value.portal) setPortal(value.portal); });
    return () => { live = false; };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const license = status?.state === "completed" ? status.license : undefined;
  const state = license ? licenseState(license) : undefined;
  const primary: Primary = status?.state === "empty" ? "activate" : state?.primary ?? null;

  // A task refused for want of a license opens the flow once, as soon as
  // this visit has read this computer's license.
  const handled = useRef(0);
  useEffect(() => {
    if (activateRequest === 0 || activateRequest === handled.current || !read) return;
    handled.current = activateRequest;
    onActivateHandled?.();
    // The action this license's state calls for; with none, there is
    // nothing to install and the task's own refusal stands.
    if (primary === null) onActivationEnded?.(false);
    else setSheet(primary);
  }, [activateRequest, read]); // eslint-disable-line react-hooks/exhaustive-deps

  async function perform<T>(action: () => Promise<T>, apply: (value: T) => void) {
    if (busy) return;
    await run("working", async () => {
      setFailure(null);
      apply(await action());
    });
  }

  const endFlow = (result: InstalledLicenseResult | null) => {
    setSheet(null);
    if (result) setStatus(result);
    onActivationEnded?.(result !== null);
  };

  const actions: Record<Exclude<Primary, null>, { label: string; open: () => void }> = {
    activate: { label: "Activate", open: () => setSheet("activate") },
    renew: { label: "Renew", open: () => setSheet("renew") },
    clock: { label: "Resolve clock", open: () => setSheet("clock") },
  };
  const active = license !== undefined && !license.deactivated;
  const more: MenuItem[] = [
    ...(active && primary !== "renew" ? [{ label: "Renew", onSelect: () => setSheet("renew") }] : []),
    ...(license
      ? [{ label: "Export license", onSelect: () => void perform(exportInstalledLicense, (value) => { if (value.state === "failed") setFailure(value.reason ?? "The license was not exported."); }) }]
      : []),
    ...(active ? [{ label: "Deactivate", tone: "danger" as const, onSelect: () => setSheet("deactivate") }] : []),
    { label: "Administrator setup", onSelect: onAdministratorSetup, separated: license !== undefined },
    ...(license ? [{ label: "Details", onSelect: () => setSheet("details") }] : []),
  ];

  const rows: { label: ReactNode; value: ReactNode }[] = license && state
    ? [
        { label: "Status", value: state.status },
        { label: "Plan", value: license.plan },
        ...(license.author ? [{ label: "Licensed user", value: license.author }] : []),
        { label: "Device", value: license.device },
        ...(license.runner_pool
          ? [{
              label: "Runner pool",
              value: onOpenRunners
                ? <button type="button" className="link" onClick={onOpenRunners}>{license.runner_pool}</button>
                : license.runner_pool,
            }]
          : []),
        ...(license.term === "not-yet-valid" ? [{ label: "Starts", value: licenseDay(license.starts) }] : []),
        { label: "Expires", value: licenseDay(license.expires) },
        ...((license.term === "grace" || license.term === "expired") && license.grace_ends !== license.expires
          ? [{ label: "Grace ends", value: licenseDay(license.grace_ends) }]
          : []),
        ...(license.deactivated_at ? [{ label: "Deactivated", value: licenseDay(license.deactivated_at) }] : []),
      ]
    : [];

  return (
    <section aria-label="License" className="license-page">
      <div className="section-header">
        <h2>License</h2>
        <span className="row-actions">
          {primary && status?.state !== "empty" ? (
            <button type="button" className="primary" disabled={busy} onClick={actions[primary].open}>
              {actions[primary].label}
            </button>
          ) : null}
          {portal ? <a className="button" href={portal} target="_blank" rel="noreferrer">Manage account</a> : null}
          <IconButton label="Refresh license" icon="refresh" disabled={busy} onClick={() => void perform(licenseStatus, setStatus)} />
          <Menu label="More license actions" items={more} />
        </span>
      </div>
      {status?.state === "empty" ? (
        <EmptyState title="No license" action={<button type="button" className="primary" disabled={busy} onClick={() => setSheet("activate")}>Activate</button>} />
      ) : null}
      {status && status.state !== "completed" && status.state !== "empty" ? <p role="alert">{status.reason}</p> : null}
      {license ? <ValueRows label="License" rows={rows} /> : null}
      {failure ? <p role="alert">{failure}</p> : null}

      {sheet === "activate" || sheet === "renew" ? (
        <LicenseFlow renewal={sheet === "renew"} installed={license} onClose={() => endFlow(null)} onDone={endFlow} />
      ) : null}
      {sheet === "deactivate" && license ? (
        <DeactivateSheet device={license.device} onClose={() => setSheet(null)} onDone={(value) => { setStatus(value); setSheet(null); }} />
      ) : null}
      {sheet === "clock" ? (
        <ResolveClockSheet onClose={() => setSheet(null)} onDone={(value) => { setStatus(value); setSheet(null); }} />
      ) : null}
      {license ? (
        <Modal
          open={sheet === "details"}
          title="License details"
          size="small"
          onClose={() => setSheet(null)}
          footer={<div className="dialog-footer"><button type="button" onClick={() => setSheet(null)}>Close</button></div>}
        >
          <ValueRows
            rows={[
              { label: "License ID", value: license.document_id },
              { label: "Organization", value: license.organization },
              { label: "Issue", value: String(license.sequence) },
              { label: "Author seats", value: String(license.author_seats) },
              { label: "Activated", value: licenseDay(license.activated) },
              { label: "Starts", value: licenseDay(license.starts) },
              { label: "Expires", value: licenseDay(license.expires) },
              { label: "Grace ends", value: licenseDay(license.grace_ends) },
            ]}
          />
        </Modal>
      ) : null}
    </section>
  );
}

/** What a review checked: exactly the input it was given, and its answer. */
type Reviewed = { input: { entitlement: string } | { contents: string }; result: LicenseReviewResult };

const NOT_A_RENEWAL = "This license is not a renewal of this computer's license.";

/** The one device a verified license offers a person, which is shown chosen;
 * none when it offers several or none. */
function soleDevice(document: LicenseDocumentView, author: string): string {
  const offered = document.operation_capable ? document.assignments?.find((entry) => entry.author === author)?.devices ?? [] : document.devices ?? [];
  return offered.length === 1 ? offered[0] ?? "" : "";
}

/** Activate license and Renew license: one sheet, steps License, Assignment
 * (only what the verified license requires, and never for a renewal) and
 * Review, and the final action installs exactly what the review showed. */
function LicenseFlow({
  renewal,
  installed,
  onClose,
  onDone,
}: {
  renewal: boolean;
  installed: InstalledLicenseView | undefined;
  onClose: () => void;
  onDone: (result: InstalledLicenseResult) => void;
}) {
  const [step, setStep] = useState("license");
  const [method, setMethod] = useState<"file" | "paste">("file");
  const [file, setFile] = useState<LicenseFileResult | null>(null);
  const [pasted, setPasted] = useState("");
  const [reviewed, setReviewed] = useState<Reviewed | null>(null);
  const [author, setAuthor] = useState("");
  const [device, setDevice] = useState("");
  // null until chosen; "" is the explicit choice of no runner pool.
  const [pool, setPool] = useState<string | null>(null);
  const [working, setWorking] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  // What would be reviewed now: the chosen file or the pasted text.
  const input: Reviewed["input"] | null =
    method === "file" ? (file?.path ? { entitlement: file.path } : null) : pasted.trim() !== "" ? { contents: pasted } : null;
  // Any change to what is reviewed withdraws the review: Continue verifies
  // again, so a review never stands for other bytes than the ones shown.
  const withdraw = () => {
    setReviewed(null);
    setNotice(null);
  };
  const document: LicenseDocumentView | undefined = reviewed?.result.state === "completed" ? reviewed.result.document : undefined;
  const renewing = renewal || reviewed?.result.renewal === true;

  const assignments = document?.assignments ?? [];
  const devices = document?.operation_capable ? assignments.find((entry) => entry.author === author)?.devices ?? [] : document?.devices ?? [];
  const pools = document?.authorities ?? [];

  async function verify(chooseKeys: boolean): Promise<SubmitFailure | null> {
    if (!input) return { reason: "Choose the license file or paste its contents." };
    const request: LicenseReviewRequest = { ...input, choose_keys: chooseKeys };
    const result = await reviewLicense(request);
    if (result.state === "cancelled") return chooseKeys ? null : { reason: result.reason ?? "No file was chosen." };
    setReviewed({ input, result });
    if (result.state !== "completed" || !result.document) return { reason: result.reason ?? "This license could not be verified." };
    if (renewal && !result.renewal) return { reason: NOT_A_RENEWAL };
    // A sole person or device the license assigns is shown chosen; the final
    // action is still the person's.
    const only = result.document.assignments?.length === 1 ? result.document.assignments[0] : undefined;
    const nextAuthor = result.document.operation_capable ? only?.author ?? "" : "";
    setAuthor(nextAuthor);
    setDevice(soleDevice(result.document, nextAuthor));
    setPool(null);
    return null;
  }

  const licenseStep = (
    <>
      <fieldset className="checks">
        <legend>Method</legend>
        <label className="check">
          <input type="radio" name="license-method" checked={method === "file"} onChange={() => { setMethod("file"); withdraw(); }} />
          File
        </label>
        <label className="check">
          <input type="radio" name="license-method" checked={method === "paste"} onChange={() => { setMethod("paste"); withdraw(); }} />
          Paste
        </label>
      </fieldset>
      {method === "file" ? (
        <>
          <span className="field-label" id="license-file-label">License file</span>
          <div className="value-with-action" aria-labelledby="license-file-label">
            <span className="location-value">{file?.name ?? "—"}</span>
            <button
              type="button"
              disabled={working}
              onClick={async () => {
                setWorking(true);
                try {
                  const chosen = await chooseLicenseFile();
                  if (chosen.state === "completed") {
                    setFile(chosen);
                    withdraw();
                  } else if (chosen.state === "failed") setNotice(chosen.reason ?? null);
                } finally {
                  setWorking(false);
                }
              }}
            >
              {file ? "Replace" : "Choose file"}
            </button>
          </div>
        </>
      ) : (
        <>
          <label htmlFor="license-contents">License contents</label>
          <textarea id="license-contents" rows={6} spellCheck={false} value={pasted} onChange={(event) => { setPasted(event.target.value); withdraw(); }} />
        </>
      )}
      {/* Only a refusal an updated keys file from the vendor can address
       * offers one; nothing is accepted without verification. */}
      {reviewed?.result.state === "failed" && reviewed.result.choose_keys ? (
        <button
          type="button"
          disabled={working}
          onClick={async () => {
            setWorking(true);
            try {
              const answer = await verify(true);
              setNotice(answer?.reason ?? null);
            } finally {
              setWorking(false);
            }
          }}
        >
          Choose verification keys
        </button>
      ) : null}
    </>
  );

  const poolField = pools.length > 0 ? (
    <>
      <label htmlFor="license-pool">Runner pool</label>
      {/* Options are told apart by position, so no pool's name can be
       * mistaken for the choice of none. */}
      <select
        id="license-pool"
        value={pool === null ? "" : pool === "" ? "none" : `pool-${pools.findIndex((entry) => entry.id === pool)}`}
        onChange={(event) => setPool(event.target.value === "none" ? "" : pools[Number(event.target.value.slice("pool-".length))]?.id ?? null)}
      >
        <option value="" disabled>Choose</option>
        {pools.map((entry, index) => <option key={entry.id} value={`pool-${index}`}>{entry.id}</option>)}
        <option value="none">None</option>
      </select>
      {pool === "" ? <p className="consequence">This device will not run tests.</p> : null}
    </>
  ) : null;

  const assignmentStep = (
    <>
      {document?.operation_capable ? (
        <>
          <label htmlFor="license-user">Licensed user</label>
          <select
            id="license-user"
            value={author}
            onChange={(event) => {
              setAuthor(event.target.value);
              setDevice(document ? soleDevice(document, event.target.value) : "");
            }}
          >
            {assignments.length > 1 ? <option value="" disabled>Choose</option> : null}
            {assignments.map((entry) => <option key={entry.author} value={entry.author}>{entry.author}</option>)}
          </select>
        </>
      ) : null}
      <label htmlFor="license-device">Device</label>
      <select id="license-device" value={device} disabled={devices.length === 0} onChange={(event) => setDevice(event.target.value)}>
        {devices.length !== 1 ? <option value="" disabled>Choose</option> : null}
        {devices.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      {document?.operation_capable ? poolField : null}
    </>
  );

  const chosenPool = pool ?? "";
  const reviewRows: { label: string; value: string }[] = document
    ? [
        { label: "Plan", value: document.plan },
        { label: "Organization", value: document.organization },
        ...(renewing
          ? [
              ...(installed?.author ? [{ label: "Licensed user", value: installed.author }] : []),
              { label: "Device", value: installed?.device ?? "—" },
              ...(installed?.current_format ? [{ label: "Runner pool", value: installed.runner_pool || "None" }] : []),
            ]
          : [
              ...(document.operation_capable ? [{ label: "Licensed user", value: author }] : []),
              { label: "Device", value: device },
              ...(document.operation_capable ? [{ label: "Runner pool", value: chosenPool || "None" }] : []),
            ]),
        { label: "Starts", value: licenseDay(document.not_before) },
        { label: "Expires", value: licenseDay(document.expires) },
        ...(document.grace_days ? [{ label: "Grace ends", value: licenseDay(document.grace_ends) }] : []),
        { label: "New work", value: document.operation_capable ? "Available" : "Not available" },
      ]
    : [];

  const assignmentValid = device !== "" && (!document?.operation_capable || (author !== "" && (pools.length === 0 || pool !== null)));
  const steps: FlowStep[] = [
    {
      key: "license",
      label: "License",
      valid: input !== null && !working,
      render: () => licenseStep,
      // A review that still stands for this input goes on; otherwise the
      // exact input is verified now, and only a verified license goes on.
      advance: async () => {
        setNotice(null);
        if (reviewed && document && JSON.stringify(reviewed.input) === JSON.stringify(input)) {
          return renewal && !reviewed.result.renewal ? { reason: NOT_A_RENEWAL } : null;
        }
        return verify(false);
      },
    },
    ...(renewing ? [] : [{ key: "assignment", label: "Assignment", valid: assignmentValid, render: () => assignmentStep }]),
    { key: "review", label: "Review", valid: document !== undefined, render: () => <ValueRows label="Review" rows={reviewRows} /> },
  ];
  // A withdrawn review sends the flow back to the license.
  const at = steps.some((entry) => entry.key === step) && (step === "license" || document) ? step : "license";

  return (
    <StepDialog
      open
      title={renewing ? "Renew license" : "Activate license"}
      steps={steps}
      step={at}
      onStep={setStep}
      nextLabel="Continue"
      submitLabel={renewing ? "Install renewal" : "Activate"}
      submitDisabled={!document || (!renewing && !assignmentValid)}
      busy={working}
      // Typed license text is not thrown away without asking.
      dirty={method === "paste" && pasted.trim() !== ""}
      // A choice or verification under way finishes before the sheet closes.
      onClose={() => { if (!working) onClose(); }}
      status={notice ? <p role="alert">{notice}</p> : null}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        if (!reviewed || !document || !reviewed.result.digest) return { reason: "Review the license first." };
        // Exactly the reviewed input and the assignment shown in the review;
        // a renewal keeps this computer's own.
        const result = await activateLicense({
          ...reviewed.input,
          trust: reviewed.result.trust ?? "",
          digest: reviewed.result.digest,
          author: renewing ? "" : author,
          device: renewing ? "" : device,
          authority: renewing ? "" : chosenPool,
        });
        if (result.state !== "completed") return { reason: result.reason ?? "The license was not activated." };
        onDone(result);
        return null;
      }}
    />
  );
}

/** Deactivate: names this device and says the one consequence. */
function DeactivateSheet({ device, onClose, onDone }: { device: string; onClose: () => void; onDone: (value: InstalledLicenseResult) => void }) {
  return (
    <FormDialog
      open
      title="Deactivate this device?"
      size="small"
      tone="danger"
      submitLabel="Deactivate"
      onClose={onClose}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        const result = await deactivateLicense();
        if (result.state !== "completed") return { reason: result.reason ?? "This device was not deactivated." };
        onDone(result);
        return null;
      }}
    >
      <ValueRows rows={[{ label: "Device", value: device }]} />
      <p className="consequence">Stops new licensed work on this device; existing evidence stays readable.</p>
    </FormDialog>
  );
}

/** Resolve clock: only once the clock is correct; it sets no clock. */
function ResolveClockSheet({ onClose, onDone }: { onClose: () => void; onDone: (value: InstalledLicenseResult) => void }) {
  return (
    <FormDialog
      open
      title="Resolve clock change"
      size="small"
      submitLabel="Resolve"
      onClose={onClose}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        const result = await resolveLicenseClock();
        if (result.state !== "completed") return { reason: result.reason ?? "The clock change was not resolved." };
        onDone(result);
        return null;
      }}
    >
      <p className="consequence">Resumes new licensed work on this device.</p>
    </FormDialog>
  );
}
