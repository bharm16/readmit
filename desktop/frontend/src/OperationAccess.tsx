import { useEffect, useState } from "react";
import { ComputerLicense } from "./ComputerLicense";
import {
  activateOperations, chooseCommercialDestinations, chooseLicenseFolder, chooseOperationPolicy,
  commercialStatus, createLicenseActivation, exportLicenseDocument, operationStatus, releaseOperations,
  renewLicenseDocument, resolveOperationClock, settleRunnerAdmission, showRunnerAdmissions,
  verifyLicenseDocument,
  type CommercialStatusResult, type LicenseExportResult, type LicenseVerifyResult,
  type OperationResult, type RunnerStatusResult,
} from "./bindings";

/** The empty role selections: an unused author/device or authority pair is
 * explicitly empty, exactly as the operation policy contract requires. */
const NONE = "(none)";

/** Activation affects new work only. The viewer and frozen practice are free.
 * The pane is license management: it verifies, configures, renews, exports,
 * settles runner capacity and shows the commercial destination, and it never
 * issues an entitlement or contacts a service on its own. */
export function OperationAccess() {
  const [result, setResult] = useState<OperationResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  const [verified, setVerified] = useState<LicenseVerifyResult | null>(null);
  const [author, setAuthor] = useState(NONE);
  const [device, setDevice] = useState(NONE);
  const [authority, setAuthority] = useState(NONE);
  const [folder, setFolder] = useState<string | null>(null);

  const [exported, setExported] = useState<LicenseExportResult | null>(null);
  const [runners, setRunners] = useState<RunnerStatusResult | null>(null);
  const [commercial, setCommercial] = useState<CommercialStatusResult | null>(null);

  useEffect(() => {
    let active = true;
    void operationStatus().then(value => { if (active) setResult(value); });
    void commercialStatus().then(value => { if (active) setCommercial(value); });
    return () => { active = false; };
  }, []);

  async function perform<T>(action: () => Promise<T>, apply: (value: T) => void) {
    if (busy) return;
    setBusy(true);
    setNotice(null);
    try { apply(await action()); } finally { setBusy(false); }
  }

  function showOperationResult(value: OperationResult) {
    if (value.state === "completed") setResult(value);
    else setNotice(value.reason ?? null);
  }

  const document = verified?.state === "completed" ? verified.document : undefined;
  const assignment = document?.assignments?.find(entry => entry.author === author);
  const devices = assignment?.devices ?? [];
  const roleComplete = (author === NONE) === (device === NONE);
  const configured = document !== undefined && folder !== null && roleComplete;

  return <section aria-labelledby="operation-access-title">
    <h3 id="operation-access-title">License and trial activation</h3>
    <p>Try the guided synthetic sample without a license. Open, verify and export existing work anytime. To create or run other work, activate the license your vendor delivered on this computer. A trial evaluation and a purchased license both arrive the same way: request one in the commercial portal below, then activate the license file when it is delivered; a trial extension is a renewal of that license, not a second one.</p>
    <ComputerLicense
      portal={commercial?.state === "completed" ? commercial.portal : undefined}
      onChanged={() => { void operationStatus().then(setResult); }}
    />
    <h4>Activation folders supplied by an administrator</h4>
    <p>An administrator can instead supply a prepared activation folder for this window, or build one from a received license and its trust document below.</p>
    <div role="status" aria-live="polite">
      {result?.reason ? <p>{result.reason}</p> : null}
      {result?.term ? <p>License: {result.term}. Organization: {result.clock?.organization}. Named authors: {result.author_seats}; runner instances: {result.runner_instances}. Expires: {result.expires}. Grace ends: {result.grace_ends}.</p> : null}
      {result?.clock ? <p>
        Latest recorded UTC time: <time>{result.clock.high_water}</time>.
        {result.clock.rollback ? " Clock correction requires explicit resolution." : " No unresolved clock rollback."}
        {result.clock.released ? " This activation is released." : " Local activation is retained; the signed term is checked for each new operation."}
      </p> : null}
      {notice ? <p>{notice}</p> : null}
    </div>
    <div className="operation-actions">
      <button disabled={busy} onClick={() => void perform(verifyLicenseDocument, value => {
        setVerified(value);
        setAuthor(NONE);
        setDevice(NONE);
        setAuthority(NONE);
        setFolder(null);
      })}>Verify a received license…</button>
      <button disabled={busy || !result?.selected} onClick={() => void perform(activateOperations, showOperationResult)}>Activate license</button>
      <button disabled={busy} onClick={() => void perform(operationStatus, setResult)}>Refresh local status</button>
      <button disabled={busy || !result?.clock?.rollback} onClick={() => void perform(resolveOperationClock, showOperationResult)}>Resolve corrected clock</button>
      <button disabled={busy || !result?.clock || result.clock.released} onClick={() => void perform(releaseOperations, showOperationResult)}>Release this activation</button>
      <button disabled={busy} onClick={() => void perform(renewLicenseDocument, value => {
        if (value.state === "completed") {
          setResult(value);
          setNotice("A renewal or an approved extension installs here: choose the later issue the vendor signed. Transfers are refused; import them where they apply.");
        } else setNotice(value.reason ?? null);
      })}>Renew or extend with a later issue…</button>
      <button disabled={busy} onClick={() => void perform(exportLicenseDocument, setExported)}>Export the installed entitlement…</button>
      <button disabled={busy} onClick={() => void perform(showRunnerAdmissions, setRunners)}>Show runner capacity</button>
      <button disabled={busy} onClick={() => void perform(async () => {
        const choice = await chooseOperationPolicy();
        return choice.state === "completed" ? operationStatus() : choice;
      }, showOperationResult)}>Select a supplied activation folder…</button>
    </div>

    {verified ? (
      <div className="license-verified">
        <h4>Received license</h4>
        {verified.state === "completed" && document ? <>
          <dl>
            <dt>Document</dt><dd>{document.id} ({document.operation_capable ? "current format" : "earlier format"})</dd>
            <dt>Organization</dt><dd>{document.organization}</dd>
            <dt>Plan</dt><dd>{document.plan}</dd>
            <dt>Term</dt><dd>{document.not_before} to {document.expires}; grace {document.grace_days ?? 0} days (ends {document.grace_ends}); state {document.state}</dd>
            <dt>Scope</dt><dd>{document.seats} author seats{document.devices_per_seat ? `, ${document.devices_per_seat} devices each` : ""}; {document.runner_instances} runner instances</dd>
            <dt>Capabilities</dt><dd>{document.capabilities?.join(", ")}</dd>
            <dt>Signed by</dt><dd>{document.key_id} ({document.key_status})</dd>
          </dl>
          {document.operation_capable ? null : <p role="note">This license is in an earlier format that lists licensed computers and cannot activate new work here; ask your vendor for a license in the current format.</p>}
        </> : <p role="note">{verified.reason}</p>}
        {document?.operation_capable ? <>
          <label htmlFor="license-author">Author this device works as</label>
          <select id="license-author" value={author} disabled={busy} onChange={event => {
            setAuthor(event.target.value);
            setDevice(NONE);
          }}>
            <option value={NONE}>No author role on this device</option>
            {(document.assignments ?? []).map(entry => <option key={entry.author} value={entry.author}>{entry.author}</option>)}
          </select>
          <label htmlFor="license-device">Device to activate</label>
          <select id="license-device" value={device} disabled={busy || author === NONE} onChange={event => setDevice(event.target.value)}>
            <option value={NONE}>No author role on this device</option>
            {devices.map(name => <option key={name} value={name}>{name}</option>)}
          </select>
          <label htmlFor="license-authority">Runner authority</label>
          <select id="license-authority" value={authority} disabled={busy} onChange={event => setAuthority(event.target.value)}>
            <option value={NONE}>No runner authority on this device</option>
            {(document.authorities ?? []).map(entry => <option key={entry.id} value={entry.id}>{entry.id} ({entry.instances} instances)</option>)}
          </select>
          <button disabled={busy} onClick={() => void perform(chooseLicenseFolder, value => {
            if (value.state === "completed" && value.folder) setFolder(value.folder);
            else setNotice(value.reason ?? null);
          })}>Choose the private activation folder…</button>
          {folder ? <p>Activation folder: {folder}. The received documents and one operation policy are created there; nothing is activated until you activate explicitly.</p> : null}
          <button disabled={busy || !configured} onClick={() => void perform(() => createLicenseActivation({
            entitlement: verified.entitlement ?? "",
            trust: verified.trust ?? "",
            author: author === NONE ? "" : author,
            device: device === NONE ? "" : device,
            authority: authority === NONE ? "" : authority,
            folder: folder ?? "",
          }), value => {
            if (value.state === "completed") {
              setResult(value);
              setFolder(null);
            } else setNotice(value.reason ?? null);
          })}>Create the local activation</button>
          {!roleComplete ? <p role="note">Select the author and the device together, or neither.</p> : null}
        </> : null}
      </div>
    ) : null}

    {exported ? (
      <div className="license-exported">
        <h4>Exported entitlement</h4>
        {exported.state === "completed"
          ? <p>Document {exported.document} written to {exported.path}, byte for byte as it was received.</p>
          : <p role="note">{exported.reason}</p>}
      </div>
    ) : null}

    {runners ? (
      <div className="runner-capacity">
        <h4>Runner capacity</h4>
        {runners.state === "completed" ? <>
          <p>Organization {runners.organization}, authority {runners.authority}: {runners.active} active, {runners.stale} stale, {runners.free} free of {runners.instances} granted instances. Stale capacity is held until an operator reconciles it.</p>
          {runners.admissions?.length ? <table>
            <thead><tr><th>Instance</th><th>State</th><th>Admitted</th><th>Lease until</th><th>Settle</th></tr></thead>
            <tbody>
              {runners.admissions.map(admission => <tr key={admission.instance}>
                <td>{admission.instance}</td>
                <td>{admission.state}</td>
                <td>{admission.admitted}</td>
                <td>{admission.lease_until}</td>
                <td>
                  <button disabled={busy} onClick={() => void perform(() => settleRunnerAdmission({ instance: admission.instance, reconcile: false }), setRunners)}>Release {admission.instance}</button>
                  <button disabled={busy} onClick={() => void perform(() => settleRunnerAdmission({ instance: admission.instance, reconcile: true }), setRunners)}>Reconcile {admission.instance}</button>
                </td>
              </tr>)}
            </tbody>
          </table> : <p>No execution instance is admitted against this authority.</p>}
        </> : <p role="note">{runners.reason}</p>}
      </div>
    ) : null}

    <div className="commercial-access">
      <h4>Commercial account and checkout</h4>
      <p>Purchases, invoices, renewals and cancellations happen in the merchant of record's hosted portal, a separate vendor service. This application makes no request to it and sends nothing from your evidence: a destination below carries no case names, endpoint values, evidence hashes or patient data.</p>
      {commercial?.state === "completed" && commercial.portal ? <>
        <p>Environment: {commercial.environment}. Destination: <span className="commercial-destination">{commercial.portal}</span></p>
        <p><a href={commercial.portal} target="_blank" rel="noreferrer">Open the commercial portal in your browser</a></p>
      </> : <p role="note">{commercial?.reason ?? "the commercial portal destination is not configured; choose the operator-supplied destinations file"}</p>}
      <button disabled={busy} onClick={() => void perform(chooseCommercialDestinations, setCommercial)}>Choose the commercial destinations file…</button>
      <ul>
        <li>Completing a payment does not activate anything here. Licensed work begins when the signed entitlement the vendor delivers is verified and imported above.</li>
        <li>Returning from checkout with nothing delivered, a cancelled payment, or a pending issuance leaves everything here unchanged: nothing is deleted, and existing work stays readable, verifiable and exportable. Import the document when it arrives, or retry.</li>
        <li>Offline limit: a revocation issued after a document was signed cannot be observed locally; it is learned when updated trust or entitlement files arrive.</li>
      </ul>
    </div>
  </section>;
}
